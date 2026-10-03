import { definePluginEntry } from "openclaw/plugin-sdk/plugin-entry";
import { runArchiveWorker } from "../archive-worker.mjs";
export default definePluginEntry({
  id: "atrium-archive-worker",
  name: "Atrium Archive Worker",
  description: "Private bounded archive extraction command",
  register(api) {
    // CLI discovery uses the public registerMode contract. There is deliberately
    // no full-only gate: both CLI registration and execution discover this command.
    api.registerCli(
      ({ program }) => {
        program
          .command("atrium-archive-worker")
          .description("Private archive extraction worker")
          .option("--json")
          .action(async () => {
            await runArchiveWorker(api);
          });
      },
      {
        descriptors: [
          {
            name: "atrium-archive-worker",
            description: "Private archive extraction worker",
            hasSubcommands: false,
            machineOutput: () => true,
          },
        ],
      },
    );
  },
});
