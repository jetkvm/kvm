import * as fs from "fs";
import * as path from "path";
import {
  sshExec,
  resetConfigViaSSH,
  restartAppViaSSH,
  saveSSHDevState,
  restoreSSHDevState,
  restoreOriginalConfig,
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

  try {
    if (await restoreOriginalConfig()) {
      await restartAppViaSSH();
      console.log("[global-teardown] Original device config restored.");
      return;
    }
    console.log("[global-teardown] No original config backup; resetting to clean state...");
    const saved = await saveSSHDevState();
    await resetConfigViaSSH();
    await restoreSSHDevState(saved);
    await restartAppViaSSH();
    console.log("[global-teardown] Device reset complete.");
  } catch {
    console.log("[global-teardown] Device cleanup failed (best-effort).");
  }
}

function hasTestFailures(resultsDir: string): boolean {
  if (!fs.existsSync(resultsDir)) return false;
  // Playwright creates per-test subdirectories in test-results/ for failed tests
  const entries = fs.readdirSync(resultsDir, { withFileTypes: true });
  return entries.some(e => e.isDirectory());
}
