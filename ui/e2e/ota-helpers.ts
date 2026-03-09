/**
 * OTA Test Helpers
 *
 * Shared infrastructure for OTA E2E tests. Provides:
 * - Mock update server (Node.js HTTP) that serves binary + optional signature
 * - Binary deployment to device via SSH
 * - Device config modification for update API URL
 *
 * Each OTA test uses these helpers in its beforeAll/afterAll to set up
 * exactly what it needs -- no hidden shell-script assumptions.
 */

import * as http from "http";
import * as fs from "fs";
import * as crypto from "crypto";
import * as os from "os";
import { execSync } from "child_process";
import { getDeviceHost, sshExec, rebootDeviceViaSSH } from "./helpers";

// ============================================================================
// Mock Update Server
// ============================================================================

export interface MockUpdateServerConfig {
  /** Absolute path to the binary file to serve for app updates */
  binaryPath: string;
  /** Version string to advertise in the /releases response */
  version: string;
  /** If set, include appSigUrl in /releases response and serve the sig file */
  signaturePath?: string;
  /** Port to listen on (default: 0 = auto-assign) */
  port?: number;
}

export interface MockUpdateServer {
  /** Full URL reachable from the device, e.g. http://192.168.1.50:12345 */
  url: string;
  /** Port the server is listening on */
  port: number;
  /** Gracefully shut down the server */
  close: () => Promise<void>;
  /** Start including appSigUrl in /releases and serve the signature file */
  enableSignature: (sigPath: string) => void;
  /** Stop including appSigUrl in /releases */
  disableSignature: () => void;
}

/**
 * Start a mock update server that mimics the JetKVM update API.
 *
 * Handles:
 * - GET /releases              -> JSON metadata (version, URLs, hash, optional sig URL)
 * - GET /app/<ver>/jetkvm_app  -> streams the binary
 * - GET /app/<ver>/jetkvm_app.sig -> streams the signature (when enabled)
 *
 * For custom version requests (query has appVersion/systemVersion), the response
 * uses the requested appVersion but always points to the same binary.
 */
export async function createMockUpdateServer(
  config: MockUpdateServerConfig,
): Promise<MockUpdateServer> {
  const { binaryPath, version } = config;
  const port = config.port ?? 0;

  if (!fs.existsSync(binaryPath)) {
    throw new Error(`Binary not found: ${binaryPath}`);
  }

  const binaryHash = await computeFileHash(binaryPath);
  const localIP = getLocalNetworkIP();
  const timestamp = Date.now();

  let signaturePath: string | undefined = config.signaturePath;

  const server = http.createServer((req, res) => {
    const url = new URL(req.url!, `http://localhost`);

    if (url.pathname === "/releases") {
      handleReleasesRequest(url, res);
    } else if (url.pathname === `/app/${version}/jetkvm_app`) {
      console.log("Streaming binary at", binaryPath);
      streamFile(binaryPath, res);
    } else if (url.pathname === `/app/${version}/jetkvm_app.sig` && signaturePath) {
      console.log("Streaming signature at", signaturePath);
      streamFile(signaturePath, res);
    } else {
      res.writeHead(404);
      res.end("Not found");
    }
  });

  function handleReleasesRequest(url: URL, res: http.ServerResponse) {
    const query = Object.fromEntries(url.searchParams);
    const isCustomVersion = "appVersion" in query || "systemVersion" in query;
    const appVersion = isCustomVersion ? (query.appVersion ?? version) : version;

    const actualPort = (server.address() as { port: number }).port;

    const response: Record<string, unknown> = {
      appVersion,
      appUrl: `http://${localIP}:${actualPort}/app/${version}/jetkvm_app`,
      appHash: binaryHash,
      appCachedAt: timestamp,
      appMaxSatisfying: "*",
      // Keep system at a low version so no system update is triggered
      systemVersion: "0.0.1",
      systemUrl: "",
      systemHash: "",
      systemCachedAt: timestamp,
      systemMaxSatisfying: "*",
    };

    if (signaturePath) {
      response.appSigUrl = `http://${localIP}:${actualPort}/app/${version}/jetkvm_app.sig`;
    }

    const body = JSON.stringify(response);
    res.writeHead(200, {
      "Content-Type": "application/json",
      "Content-Length": Buffer.byteLength(body),
    });
    res.end(body);
  }

  function streamFile(filePath: string, res: http.ServerResponse) {
    const stat = fs.statSync(filePath);
    res.writeHead(200, {
      "Content-Length": stat.size,
      "Content-Type": "application/octet-stream",
    });
    fs.createReadStream(filePath).pipe(res);
  }

  // Start listening
  await new Promise<void>((resolve, reject) => {
    server.on("error", reject);
    server.listen(port, "0.0.0.0", () => resolve());
  });

  const actualPort = (server.address() as { port: number }).port;
  const serverUrl = `http://${localIP}:${actualPort}`;

  return {
    url: serverUrl,
    port: actualPort,
    close: () =>
      new Promise<void>((resolve, reject) => {
        server.close(err => (err ? reject(err) : resolve()));
      }),
    enableSignature: (sigPath: string) => {
      console.log("Enabling signature at", sigPath);
      signaturePath = sigPath;
    },
    disableSignature: () => {
      console.log("Disabling signature");
      signaturePath = undefined;
    },
  };
}

// ============================================================================
// Binary Deployment
// ============================================================================

/**
 * Deploy a binary to the device via SSH.
 * Copies the file to /userdata/jetkvm/jetkvm_app.update which the device
 * picks up on next boot.
 *
 * @param binaryPath - Absolute path to the binary on the dev machine
 */
export async function deployBinaryToDevice(binaryPath: string): Promise<void> {
  if (!fs.existsSync(binaryPath)) {
    throw new Error(`Binary not found: ${binaryPath}`);
  }

  const host = getDeviceHost();
  const { exec } = await import("child_process");
  const { promisify } = await import("util");
  const execAsync = promisify(exec);

  const sshCmd = [
    "ssh",
    "-o UserKnownHostsFile=/dev/null",
    "-o StrictHostKeyChecking=no",
    "-o ConnectTimeout=10",
    `root@${host}`,
    '"cat > /userdata/jetkvm/jetkvm_app.update"',
  ].join(" ");
  await execAsync(`${sshCmd} < "${binaryPath}"`);
}

// ============================================================================
// Device Config Helpers
// ============================================================================

const PRODUCTION_API_URL = "https://api.jetkvm.com";

/**
 * Configure the device to use a custom update API URL.
 * Modifies /userdata/kvm_config.json via SSH.
 *
 * @param url - The mock server URL to point the device to
 */
export async function configureDeviceUpdateUrl(url: string): Promise<void> {
  await sshExec(
    `sed -i "s|\\"update_api_url\\": \\"[^\\"]*\\"|\\"update_api_url\\": \\"${url}\\"|" /userdata/kvm_config.json`,
  );
}

/**
 * Restore the device update API URL to the production endpoint.
 * Best-effort -- does not throw on failure.
 */
export async function restoreDeviceUpdateUrl(): Promise<void> {
  try {
    await configureDeviceUpdateUrl(PRODUCTION_API_URL);
  } catch {
    // Best-effort cleanup
  }
}

/**
 * Set include_pre_release in the device config.
 */
export async function setIncludePreRelease(value: boolean): Promise<void> {
  await sshExec(
    `sed -i "s|\\"include_pre_release\\": [^,]*|\\"include_pre_release\\": ${value}|" /userdata/kvm_config.json`,
  );
}

// ============================================================================
// Utilities
// ============================================================================

/**
 * Compute the SHA-256 hash of a file.
 */
export async function computeFileHash(filePath: string): Promise<string> {
  return new Promise((resolve, reject) => {
    const hash = crypto.createHash("sha256");
    const stream = fs.createReadStream(filePath);
    stream.on("data", data => hash.update(data));
    stream.on("end", () => resolve(hash.digest("hex")));
    stream.on("error", reject);
  });
}

/**
 * Detect the local network IP address that the device can reach.
 * Prefer route-based detection (same approach as previous shell scripts),
 * then fall back to the first non-internal IPv4 address.
 */
export function getLocalNetworkIP(): string {
  try {
    const routeOutput = execSync("ip route get 1", {
      encoding: "utf8",
      stdio: ["ignore", "pipe", "ignore"],
    });
    const routeMatch = routeOutput.match(/\bsrc\s+(\d+\.\d+\.\d+\.\d+)\b/);
    if (routeMatch?.[1]) {
      return routeMatch[1];
    }
  } catch {
    // Fall through to interface scan if route-based detection is unavailable.
  }

  const interfaces = os.networkInterfaces();
  for (const name of Object.keys(interfaces)) {
    for (const iface of interfaces[name]!) {
      if (iface.family === "IPv4" && !iface.internal) {
        return iface.address;
      }
    }
  }
  throw new Error("Could not detect local network IP address");
}
