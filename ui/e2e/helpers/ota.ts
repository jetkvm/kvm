import * as http from "http";
import * as https from "https";
import * as fs from "fs";
import * as crypto from "crypto";
import * as os from "os";
import * as path from "path";
import { execSync } from "child_process";
import { expect } from "@playwright/test";
import type { Page } from "@playwright/test";
import { sshExec } from "./ssh";

export interface MockUpdateServerConfig {
  binaryPath: string;
  version: string;
  signaturePath?: string;
  port?: number;
}

export interface MockUpdateServer {
  url: string;
  port: number;
  close: () => Promise<void>;
  enableSignature: (sigPath: string) => void;
  disableSignature: () => void;
}

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
      streamFile(binaryPath, res);
    } else if (url.pathname === `/app/${version}/jetkvm_app.sig` && signaturePath) {
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
      signaturePath = sigPath;
    },
    disableSignature: () => {
      signaturePath = undefined;
    },
  };
}

const PRODUCTION_API_URL = "https://api.jetkvm.com";

export async function configureDeviceUpdateUrl(url: string): Promise<void> {
  await sshExec(
    `sed -i "s|\\"update_api_url\\": \\"[^\\"]*\\"|\\"update_api_url\\": \\"${url}\\"|" /userdata/kvm_config.json`,
  );
}

export async function restoreDeviceUpdateUrl(): Promise<void> {
  try {
    await configureDeviceUpdateUrl(PRODUCTION_API_URL);
  } catch {
    // Best-effort cleanup
  }
}

export async function setIncludePreRelease(value: boolean): Promise<void> {
  await sshExec(
    `sed -i "s|\\"include_pre_release\\": [^,]*|\\"include_pre_release\\": ${value}|" /userdata/kvm_config.json`,
  );
}

export async function computeFileHash(filePath: string): Promise<string> {
  return new Promise((resolve, reject) => {
    const hash = crypto.createHash("sha256");
    const stream = fs.createReadStream(filePath);
    stream.on("data", data => hash.update(data));
    stream.on("end", () => resolve(hash.digest("hex")));
    stream.on("error", reject);
  });
}

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
    // Fall through to interface scan
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

export interface StableReleaseInfo {
  appVersion: string;
  appUrl: string;
  appHash: string;
  appSigUrl?: string;
}

export async function fetchLatestStableRelease(): Promise<StableReleaseInfo> {
  const url = "https://api.jetkvm.com/releases?deviceId=e2e-test";
  const body = await new Promise<string>((resolve, reject) => {
    https
      .get(url, res => {
        if (res.statusCode !== 200) {
          reject(new Error(`Release API returned ${res.statusCode}`));
          res.resume();
          return;
        }
        let data = "";
        res.on("data", chunk => (data += chunk));
        res.on("end", () => resolve(data));
        res.on("error", reject);
      })
      .on("error", reject);
  });

  const json = JSON.parse(body);
  if (!json.appVersion || !json.appUrl || !json.appHash) {
    throw new Error(`Unexpected release API response: ${body}`);
  }
  return {
    appVersion: json.appVersion,
    appUrl: json.appUrl,
    appHash: json.appHash,
    appSigUrl: json.appSigUrl,
  };
}

export async function downloadFile(url: string, destPath: string): Promise<void> {
  const proto = url.startsWith("https") ? https : http;
  const file = fs.createWriteStream(destPath);

  await new Promise<void>((resolve, reject) => {
    const request = (requestUrl: string) => {
      proto
        .get(requestUrl, res => {
          if (
            res.statusCode &&
            res.statusCode >= 300 &&
            res.statusCode < 400 &&
            res.headers.location
          ) {
            request(res.headers.location);
            return;
          }
          if (res.statusCode !== 200) {
            reject(new Error(`Download failed: ${res.statusCode} for ${requestUrl}`));
            res.resume();
            return;
          }
          res.pipe(file);
          file.on("finish", () => file.close(() => resolve()));
          res.on("error", reject);
        })
        .on("error", reject);
    };
    request(url);
  });
}

export interface OTAEnvVars {
  baselinePath: string;
  releasePath: string;
  releaseVersion: string;
  signaturePath?: string;
}

export function getOTAEnvVars(opts?: { requireSignature?: boolean }): OTAEnvVars {
  const baselinePath = process.env.BASELINE_BINARY_PATH;
  const releasePath = process.env.RELEASE_BINARY_PATH;
  const releaseVersion = process.env.TEST_UPDATE_VERSION;
  const signaturePath = process.env.RELEASE_SIGNATURE_PATH;

  if (!baselinePath) throw new Error("BASELINE_BINARY_PATH is required");
  if (!releasePath) throw new Error("RELEASE_BINARY_PATH is required");
  if (!releaseVersion) throw new Error("TEST_UPDATE_VERSION is required");
  if (opts?.requireSignature && !signaturePath) {
    throw new Error("RELEASE_SIGNATURE_PATH is required");
  }

  return { baselinePath, releasePath, releaseVersion, signaturePath };
}

export function toPreReleaseVersion(version: string): string {
  return version.includes("-") ? version : `${version}-dev.1`;
}

/**
 * Navigate to the update page, dismiss a cached error (Retry button) if present,
 * and click "Update Now".
 */
export async function triggerUpdate(page: Page): Promise<void> {
  await page.goto("/settings/general/update");
  await page.waitForLoadState("networkidle");

  const retryButton = page.getByRole("button", { name: "Retry" });
  if (await retryButton.isVisible({ timeout: 5000 }).catch(() => false)) {
    await retryButton.click();
  }

  const updateButton = page.getByRole("button", { name: "Update Now" });
  await expect(updateButton).toBeVisible({ timeout: 30000 });
  await updateButton.click();
}

/**
 * Create a temporary signature file, run the callback with the mock server
 * configured to serve it, then clean up.
 */
export async function withTempSignature(
  mockServer: MockUpdateServer,
  content: Buffer,
  fn: () => Promise<void>,
): Promise<void> {
  const sigPath = path.join(os.tmpdir(), `e2e_sig_${Date.now()}.sig`);
  fs.writeFileSync(sigPath, content);
  try {
    mockServer.enableSignature(sigPath);
    await fn();
  } finally {
    mockServer.disableSignature();
    fs.unlinkSync(sigPath);
  }
}

/**
 * Compare two semver strings (ignoring prerelease suffixes).
 * Returns true if `version` >= `minimum`.
 */
export function semverGte(version: string, minimum: string): boolean {
  const v = version.replace(/^v/, "").split("-")[0].split(".").map(Number);
  const m = minimum.split(".").map(Number);
  for (let i = 0; i < 3; i++) {
    if ((v[i] ?? 0) > (m[i] ?? 0)) return true;
    if ((v[i] ?? 0) < (m[i] ?? 0)) return false;
  }
  return true;
}
