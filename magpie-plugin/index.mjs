// AgyMate native page plugin entry for Magpie.
// The interactive console is served via `magpie.page` (`page/index.html` + `page/boot.js`)
// and connects to the local AgyMate Hub daemon (`127.0.0.1:<port>`, default 3778).

export const DEFAULT_HUB_PORT = 3778;

export function resolveHubPort(options) {
  const raw = Number(options?.port);
  if (Number.isInteger(raw) && raw >= 1 && raw <= 65535) {
    return raw;
  }
  return DEFAULT_HUB_PORT;
}

export default async function agymatePlugin() {
  return {};
}
