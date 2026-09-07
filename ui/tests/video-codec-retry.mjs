import assert from "node:assert/strict";
import { fileURLToPath } from "node:url";
import { rolldown } from "rolldown";
import { chromium } from "playwright";

const entry = fileURLToPath(new URL("./video-codec-entry.tsx", import.meta.url));
const route = fileURLToPath(
  new URL("../src/routes/devices.$id.settings.video.tsx", import.meta.url),
);
const bundle = await rolldown({
  input: entry,
  transform: { define: { "process.env.NODE_ENV": '"production"' } },
  plugins: [
    {
      name: "video-settings-harness",
      resolveId(source) {
        if (source === entry || source === route) return source;
        if (source.startsWith("@")) return "\0" + source;
      },
      load(id) {
        if (id === entry)
          return `
        import { createElement } from 'react';
        import { createRoot } from 'react-dom/client';
        import Route from ${JSON.stringify(route)};
        window.requests = [];
        window.send = (method, params, callback) => {
          if (method === 'getSupportedVideoCodecs') window.requests.push(callback);
        };
        createRoot(document.getElementById('root')).render(createElement(Route));
      `;
        if (id === "\0@/hooks/useJsonRpc")
          return "export const useJsonRpc = () => ({send: window.send});";
        if (id === "\0@hooks/stores")
          return "export const useSettingsStore = () => ({videoSaturation: 50, videoBrightness: 50, videoContrast: 50});";
        if (id === "\0@/utils") return "export const isLinuxDesktop = () => false;";
        if (id === "\0@/notifications") return "export default {error() {}, success() {}};";
        if (id === "\0@localizations/messages.js")
          return "export const m = new Proxy({}, {get: (_, key) => () => key});";
        if (id === "\0@components/SelectMenuBasic")
          return `
        import {createElement as h} from 'react';
        export const SelectMenuBasic = ({options, disabled, value, onChange}) =>
          h('select', {disabled, value, onChange}, options.map(o => h('option', {key:o.value, value:o.value, disabled:o.disabled}, o.label)));
      `;
        if (id === "\0@components/Button")
          return `
        import {createElement as h} from 'react';
        export const Button = ({text, onClick, disabled}) => h('button', {onClick, disabled}, text);
      `;
        if (id.startsWith("\0@components/")) {
          const name = id.split("/").pop();
          const exported =
            { SettingsPageheader: "SettingsPageHeader", TextArea: "TextAreaWithLabel" }[name] ||
            name;
          return `import {createElement as h} from 'react';
          const Component = ({children,title,loading}) => h('div', {'data-title':title,'data-loading':String(!!loading)}, children);
          export {Component as ${exported}}; export default Component;`;
        }
      },
    },
  ],
});
const { output } = await bundle.generate({ format: "iife" });
await bundle.close();
const browser = await chromium.launch({
  executablePath: process.env.CHROME_BIN,
  headless: true,
  args: ["--no-sandbox"],
});
try {
  const page = await browser.newPage();
  page.on("pageerror", error => console.error(error));
  await page.clock.install();
  await page.setContent('<div id="root"></div>');
  await page.evaluate(() => {
    RTCRtpReceiver.getCapabilities = () => ({ codecs: [{ mimeType: "video/H265" }] });
  });
  await page.addScriptTag({ content: output[0].code });
  const section = page.locator('[data-title="video_codec_title"]');
  const select = section.locator("select");
  const retry = section.getByRole("button", { name: "retry", exact: true });
  await page.waitForFunction(() => window.requests.length === 1);
  assert(await select.isDisabled());
  await page.evaluate(() =>
    window.requests[0]({ error: { code: -32000, message: "temporary failure" } }),
  );
  await retry.waitFor();
  assert.equal(await section.getAttribute("data-loading"), "false");
  await retry.click();
  await page.waitForFunction(() => window.requests.length === 2);
  await page.evaluate(() => window.requests[1]({ result: { invalid: true } }));
  await retry.waitFor();
  await retry.click();
  await page.waitForFunction(() => window.requests.length === 3);
  await page.clock.fastForward(10000);
  await retry.waitFor();
  await retry.click();
  await page.waitForFunction(() => window.requests.length === 4);
  // An older attempt must not enable the selector while its replacement is pending.
  await page.evaluate(() => window.requests[2]({ result: ["h264", "h265"] }));
  assert(await select.isDisabled());
  await page.evaluate(() => window.requests[3]({ result: ["h264"] }));
  await page.waitForFunction(
    () => !document.querySelector('[data-title="video_codec_title"] select').disabled,
  );
  assert.equal(await retry.count(), 0);
  assert.deepEqual(
    await select.locator("option").evaluateAll(options => options.map(o => o.value)),
    ["auto", "h264"],
  );
  console.log("Codec RPC error, invalid response, timeout, stale reply and retry recovery: OK");
} finally {
  await browser.close();
}
