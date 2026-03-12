import {
  sshExec,
  getDeviceHost,
  resetConfigViaSSH,
  restartAppViaSSH,
} from "./helpers";
import * as fs from "fs";
import { promisify } from "util";
import { exec } from "child_process";

const execAsync = promisify(exec);

export default async function globalSetup() {
  const binaryPath = process.env.BASELINE_BINARY_PATH;
  if (!binaryPath) {
    console.log("[global-setup] BASELINE_BINARY_PATH not set, skipping deployment.");
    return;
  }

  if (!fs.existsSync(binaryPath)) {
    throw new Error(`BASELINE_BINARY_PATH not found: ${binaryPath}`);
  }

  console.log("[global-setup] Deploying binary to device...");
  await sshExec("killall jetkvm_app", true);
  await new Promise(r => setTimeout(r, 1000));

  const host = getDeviceHost();
  const sshCmd = [
    "ssh",
    "-o UserKnownHostsFile=/dev/null",
    "-o StrictHostKeyChecking=no",
    "-o ConnectTimeout=10",
    `root@${host}`,
    '"cat > /userdata/jetkvm/bin/jetkvm_app"',
  ].join(" ");
  await execAsync(`${sshCmd} < "${binaryPath}"`);

  await sshExec("chmod +x /userdata/jetkvm/bin/jetkvm_app");
  await resetConfigViaSSH();
  await restartAppViaSSH();
  console.log("[global-setup] Device ready.");
}
