import { sshExec } from "./helpers";
import * as fs from "fs";
import * as path from "path";

export default async function globalTeardown() {
  const resultsDir = path.resolve(
    path.dirname(new URL(import.meta.url).pathname),
    "../test-results",
  );

  if (!hasTestFailures(resultsDir)) return;

  console.log("[global-teardown] Test failures detected, capturing device logs...");
  const logDir = path.join(resultsDir, "device-logs");
  fs.mkdirSync(logDir, { recursive: true });

  const logs: Record<string, string> = {
    "device-last.log": "cat /userdata/jetkvm/last.log",
    "device-config.json": "cat /userdata/kvm_config.json",
    "device-dmesg.txt": "dmesg | tail -200",
  };

  for (const [filename, cmd] of Object.entries(logs)) {
    try {
      const output = await sshExec(cmd, true);
      fs.writeFileSync(path.join(logDir, filename), output);
    } catch {
      // Best-effort
    }
  }
}

function hasTestFailures(resultsDir: string): boolean {
  if (!fs.existsSync(resultsDir)) return false;
  // Playwright creates per-test subdirectories in test-results/ for failed tests
  const entries = fs.readdirSync(resultsDir, { withFileTypes: true });
  return entries.some(e => e.isDirectory());
}
