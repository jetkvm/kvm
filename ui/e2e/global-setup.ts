import * as fs from "fs";
import { promisify } from "util";
import { exec } from "child_process";
import {
  sshExec,
  getDeviceHost,
  resetConfigViaSSH,
  restartAppViaSSH,
  saveSSHDevState,
  restoreSSHDevState,
  SSH_OPTS,
} from "./helpers";

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
  const sshCmd = `ssh ${SSH_OPTS} root@${host} "cat > /userdata/jetkvm/bin/jetkvm_app"`;
  await execAsync(`${sshCmd} < "${binaryPath}"`);

  await sshExec("chmod +x /userdata/jetkvm/bin/jetkvm_app");

  const saved = await saveSSHDevState();
  await resetConfigViaSSH();
  await restoreSSHDevState(saved);

  await restartAppViaSSH();
  console.log("[global-setup] Device ready.");
}
