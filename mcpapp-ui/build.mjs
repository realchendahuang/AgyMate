import { readFileSync, writeFileSync, mkdirSync } from "node:fs";
import { dirname, resolve } from "node:path";
import { fileURLToPath } from "node:url";

const here = dirname(fileURLToPath(import.meta.url));
const entryJS = resolve(here, "src/main.js");
const cssPath = resolve(here, "src/styles.css");
const outPath = resolve(here, "../internal/mcpapp/dist/app.html");

const result = await Bun.build({
  entrypoints: [entryJS],
  target: "browser",
  format: "iife",
  minify: true,
  bundle: true,
});

if (!result.success) {
  for (const log of result.logs) {
    console.error(log);
  }
  process.exit(1);
}

const jsCode = await result.outputs[0].text();
const cssCode = readFileSync(cssPath, "utf-8");

const safeCSS = cssCode.replace(/<\/style/gi, "<\\/style");
const safeJS = jsCode.replace(/<\/script/gi, "<\\/script");

const html = `<!DOCTYPE html>
<html lang="zh-CN">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>AgyMate</title>
  <style>
${safeCSS}
  </style>
</head>
<body>
  <div id="app" class="shell">
    <div class="boot-msg">正在连接 AgyMate 服务…</div>
  </div>
  <script>
${safeJS}
  </script>
</body>
</html>
`;

mkdirSync(dirname(outPath), { recursive: true });
writeFileSync(outPath, html, "utf-8");
console.log(`Built ${outPath} (${Buffer.byteLength(html, "utf-8")} bytes)`);
