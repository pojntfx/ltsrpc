/* eslint-disable no-console */
import { env, exit } from "process";
import { parse } from "url";
// eslint-disable-next-line import/no-extraneous-dependencies
import { Socket, createServer } from "net";
import { ILocalContext, Registry } from "./index";

let clients = 0;

const registry = new Registry(
  new (class {
    // eslint-disable-next-line class-methods-use-this
    async Iterate(
      ctx: ILocalContext,
      length: number,
      onIteration: (i: number, b: string) => Promise<string>
    ): Promise<number> {
      for (let i = 0; i < length; i++) {
        // eslint-disable-next-line no-await-in-loop
        const rv = await onIteration(i, "This is from the callee");

        console.log("Closure returned:", rv);
      }

      return length;
    }
  })(),
  new (class {})(),
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

const addr = env.ADDR || "127.0.0.1:1337";
const listen = env.LISTEN !== "false";

if (listen) {
  const u = parse(`tcp://${addr}`);

  const server = createServer(async (socket) => {
    socket.on("error", (e) => {
      console.error("Client disconnected with error:", e);
    });

    registry.linkTCPSocket(
      socket,

      JSON.stringify,
      JSON.parse,

      (v) => v,
      (v) => v
    );
  });

  server.listen(
    {
      host: u.hostname as string,
      port: parseInt(u.port as string, 10),
    },
    () => console.log("Listening on", addr)
  );
} else {
  const u = parse(`tcp://${addr}`);

  const socket = new Socket();

  socket.on("error", (e) => {
    console.error("Disconnected with error:", e.cause);

    exit(1);
  });
  socket.on("close", () => exit(0));

  await new Promise<void>((res, rej) => {
    socket.connect(
      {
        host: u.hostname as string,
        port: parseInt(u.port as string, 10),
      },
      res
    );
    socket.on("error", rej);
  });

  registry.linkTCPSocket(
    socket,

    JSON.stringify,
    JSON.parse,

    (v) => v,
    (v) => v
  );

  console.log("Connected to", addr);
}
