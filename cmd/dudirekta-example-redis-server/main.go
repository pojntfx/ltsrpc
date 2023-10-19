package main

import (
	"bufio"
	"context"
	"encoding/json"
	"flag"
	"log"
	"net"
	"os"
	"strings"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
	"github.com/pojntfx/dudirekta/pkg/rpc"
	"github.com/redis/go-redis/v9"
)

const (
	errBusyGroup             = "BUSYGROUP Consumer Group name already exists"
	errReceivedEmptyPacket   = "received empty packet"
	errReceivedInvalidPacket = "received invalid packet"
)

type local struct {
	counter int64
}

func (s *local) Increment(ctx context.Context, delta int64) (int64, error) {
	log.Println("Incrementing counter by", delta, "for peer with ID", rpc.GetRemoteID(ctx))

	return atomic.AddInt64(&s.counter, delta), nil
}

type remote struct {
	Println func(ctx context.Context, msg string) error
}

func main() {
	redisURL := flag.String("redis-url", "redis://localhost:6379/0", "Redis URL")

	clientRequestsStream := flag.String("client-requests-stream", "/conn1/requests/client", "Redis stream to write requests to client to")
	clientResponsesStream := flag.String("client-responses-stream", "/conn1/responses/client", "Redis stream to write responses to client to")

	serverRequestsStream := flag.String("server-requests-stream", "/conn1/requests/server", "Redis stream to read requests from client from")
	serverResponsesStream := flag.String("server-responses-stream", "/conn1/responses/server", "Redis stream to read responses from client from")

	timeout := flag.Duration("timeout", time.Minute, "Time to wait for responses (since the stream is reliable & guaranteed, you typically use high values)")

	flag.Parse()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	clients := 0

	registry := rpc.NewRegistry(
		&local{},
		remote{},

		*timeout,
		ctx,
		&rpc.Options{
			OnClientConnect: func(remoteID string) {
				clients++

				log.Printf("%v clients connected", clients)
			},
			OnClientDisconnect: func(remoteID string) {
				clients--

				log.Printf("%v clients connected", clients)
			},
		},
	)

	go func() {
		log.Println(`Enter one of the following letters followed by <ENTER> to run a function on the remote(s):

- a: Print "Hello, world!"`)

		stdin := bufio.NewReader(os.Stdin)

		for {
			line, err := stdin.ReadString('\n')
			if err != nil {
				panic(err)
			}

			for peerID, peer := range registry.Peers() {
				log.Println("Calling functions for peer with ID", peerID)

				switch line {
				case "a\n":
					if err := peer.Println(ctx, "Hello, world!"); err != nil {
						log.Println("Got error for Println func:", err)

						continue
					}
				default:
					log.Printf("Unknown letter %v, ignoring input", line)

					continue
				}
			}
		}
	}()

	options, err := redis.ParseURL(*redisURL)
	if err != nil {
		panic(err)
	}

	broker := redis.NewClient(options)
	defer broker.Close()

	if _, err := broker.XGroupCreateMkStream(ctx, *clientRequestsStream, *clientRequestsStream, "$").Result(); err != nil && !strings.Contains(err.Error(), errBusyGroup) {
		panic(err)
	}

	if _, err := broker.XGroupCreateMkStream(ctx, *clientResponsesStream, *clientResponsesStream, "$").Result(); err != nil && !strings.Contains(err.Error(), errBusyGroup) {
		panic(err)
	}

	if _, err := broker.XGroupCreateMkStream(ctx, *serverRequestsStream, *serverRequestsStream, "$").Result(); err != nil && !strings.Contains(err.Error(), errBusyGroup) {
		panic(err)
	}

	if _, err := broker.XGroupCreateMkStream(ctx, *serverResponsesStream, *serverResponsesStream, "$").Result(); err != nil && !strings.Contains(err.Error(), errBusyGroup) {
		panic(err)
	}

	log.Println("Connected to Redis")

	requestPackets := make(chan []byte)
	go func() {
		defer close(requestPackets)

		for {
			streams, err := broker.XReadGroup(ctx, &redis.XReadGroupArgs{
				Group:    *serverRequestsStream,
				Consumer: uuid.NewString(),
				Streams:  []string{*serverRequestsStream, ">"},
				Block:    0,
				Count:    10,
			}).Result()
			if err != nil {
				panic(err)
			}

			for _, stream := range streams {
				for _, message := range stream.Messages {
					rawPacket, ok := message.Values["packet"]
					if !ok {
						panic(errReceivedEmptyPacket)
					}

					packet, ok := rawPacket.(string)
					if !ok {
						panic(errReceivedInvalidPacket)
					}

					requestPackets <- []byte(packet)
				}
			}
		}
	}()

	responsePackets := make(chan []byte)
	go func() {
		defer close(responsePackets)

		for {
			streams, err := broker.XReadGroup(ctx, &redis.XReadGroupArgs{
				Group:    *serverResponsesStream,
				Consumer: uuid.NewString(),
				Streams:  []string{*serverResponsesStream, ">"},
				Block:    0,
				Count:    10,
			}).Result()
			if err != nil {
				panic(err)
			}

			for _, stream := range streams {
				for _, message := range stream.Messages {
					rawPacket, ok := message.Values["packet"]
					if !ok {
						panic(errReceivedEmptyPacket)
					}

					packet, ok := rawPacket.(string)
					if !ok {
						panic(errReceivedInvalidPacket)
					}

					responsePackets <- []byte(packet)
				}
			}
		}
	}()

	if err := registry.LinkMessage(
		func(b []byte) error {
			if _, err := broker.XAdd(ctx, &redis.XAddArgs{
				Stream: *clientRequestsStream,
				Values: map[string]interface{}{
					"packet": b,
				},
			}).Result(); err != nil {
				return err
			}

			return nil
		},
		func(b []byte) error {
			if _, err := broker.XAdd(ctx, &redis.XAddArgs{
				Stream: *clientResponsesStream,
				Values: map[string]interface{}{
					"packet": b,
				},
			}).Result(); err != nil {
				return err
			}

			return nil
		},

		func() ([]byte, error) {
			packet, ok := <-requestPackets
			if !ok {
				return []byte{}, net.ErrClosed
			}

			return packet, nil
		},
		func() ([]byte, error) {
			packet, ok := <-responsePackets
			if !ok {
				return []byte{}, net.ErrClosed
			}

			return packet, nil
		},

		json.Marshal,
		json.Unmarshal,
	); err != nil {
		panic(err)
	}
}
