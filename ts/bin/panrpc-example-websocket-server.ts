/* eslint-disable no-console */
import { env, exit, stdin, stdout } from "process";
import { createInterface } from "readline/promises";
import { parse } from "url";
import Chain from "stream-chain";
import p from "stream-json/jsonl/Parser";
import Stringer from "stream-json/jsonl/Stringer";
// eslint-disable-next-line import/no-extraneous-dependencies
import { WebSocket, WebSocketServer } from "ws";
import { ILocalContext, IRemoteContext, Registry } from "../index";

class Local {
  private counter = 0;

  constructor() {
    this.Increment = this.Increment.bind(this);
  }

  async Increment(ctx: ILocalContext, delta: number): Promise<number> {
    console.log(
      "Incrementing counter by",
      delta,
      "for remote with ID",
      ctx.remoteID
    );

    this.counter += delta;

    return this.counter;
  }
}

class Remote {
  // eslint-disable-next-line class-methods-use-this, @typescript-eslint/no-unused-vars, @typescript-eslint/no-empty-function
  async Println(ctx: IRemoteContext, msg: string) {}
}

let clients = 0;

const registry = new Registry(
  new Local(),
  new Remote(),

  {
    onClientConnect: () => {
      clients++;

      console.log(clients, "clients connected");
    },
    onClientDisconnect: () => {
      clients--;

      console.log(clients, "clients connected");
    },
  }
);

(async () => {
  console.log(`Enter one of the following letters followed by <ENTER> to run a function on the remote(s):

- a: Print "Hello, world!
`);

  const rl = createInterface({ input: stdin, output: stdout });

  // eslint-disable-next-line no-constant-condition
  while (true) {
    const line =
      // eslint-disable-next-line no-await-in-loop
      await rl.question("");

    // eslint-disable-next-line no-await-in-loop
    await registry.forRemotes(async (remoteID, remote) => {
      console.log("Calling functions for remote with ID", remoteID);

      switch (line) {
        case "a":
          try {
            // eslint-disable-next-line no-await-in-loop
            await remote.Println(undefined, "Hello, world!");
          } catch (e) {
            console.error(`Got error for Increment func: ${e}`);
          }

          break;

        default:
          console.log(`Unknown letter ${line}, ignoring input`);
      }
    });
  }
})();

const addr = env.ADDR || "127.0.0.1:1337";
const listen = env.LISTEN !== "false";

if (listen) {
  const u = parse(`ws://${addr}`);

  const server = new WebSocketServer({
    host: u.hostname as string,
    port: parseInt(u.port as string, 10),
  });

  server.on("connection", (socket) => {
    socket.addEventListener("error", (e) => {
      console.error("Client disconnected with error:", e);
    });

    const decoder = new Chain([p.parser(), (v) => v.value]);
    socket.addEventListener("message", (m) => decoder.write(m.data));
    socket.addEventListener("close", () => decoder.destroy());

    const encoder = new Stringer();
    encoder.pipe(new Chain([(m) => socket.send(m)]));
    socket.addEventListener("close", () => encoder.destroy());

    registry.linkStream(
      encoder,
      decoder,

      (v) => v,
      (v) => v
    );
  });

  console.log("Listening on", addr);
} else {
  const socket = new WebSocket(`ws://${addr}`);

  socket.addEventListener("error", (e) => {
    console.error("Disconnected with error:", e);

    exit(1);
  });
  socket.addEventListener("close", () => exit(0));

  await new Promise<void>((res, rej) => {
    socket.addEventListener("open", () => res());
    socket.addEventListener("error", rej);
  });

  const decoder = new Chain([p.parser(), (v) => v.value]);
  socket.addEventListener("message", (m) => decoder.write(m.data));
  socket.addEventListener("close", () => decoder.destroy());

  const encoder = new Stringer();
  encoder.pipe(new Chain([(m) => socket.send(m)]));
  socket.addEventListener("close", () => encoder.destroy());

  registry.linkStream(
    encoder,
    decoder,

    (v) => v,
    (v) => v
  );

  console.log("Connected to", addr);
}
