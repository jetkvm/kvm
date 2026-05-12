import * as fs from "fs";
import * as path from "path";
import {
  sshExec,
  resetConfigViaSSH,
  restartAppViaSSH,
  saveSSHDevState,
  restoreSSHDevState,
} from "./helpers";

export default async function globalTeardown() {
  const resultsDir = path.resolve(
    path.dirname(new URL(import.meta.url).pathname),
    "../test-results",
  );

  if (hasTestFailures(resultsDir)) {
    console.log("[global-teardown] Test failures detected, capturing device logs...");
    const logDir = path.join(resultsDir, "device-logs");
    fs.mkdirSync(logDir, { recursive: true });

    const logs: Record<string, string> = {
      "device-last.log": "cat /userdata/jetkvm/last.log",
      // Rotated by restartAppViaSSH — preserves the failing session's output
      // when a later test restarts the app before teardown captures logs.
      // sshExec(_, true) returns "" if the file is missing, so no shell guard needed.
      "device-prev.log": "cat /userdata/jetkvm/last.log.prev",
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

  console.log("[global-teardown] Resetting device to clean state...");
  try {
    const saved = await saveSSHDevState();
    await resetConfigViaSSH();
    await restoreSSHDevState(saved);
    await restartAppViaSSH();
    console.log("[global-teardown] Device reset complete.");
  } catch {
    console.log("[global-teardown] Device reset failed (best-effort).");
  }
}

function hasTestFailures(resultsDir: string): boolean {
  if (!fs.existsSync(resultsDir)) return false;
  // Playwright creates per-test subdirectories in test-results/ for failed tests
  const entries = fs.readdirSync(resultsDir, { withFileTypes: true });
  return entries.some(e => e.isDirectory());
}
