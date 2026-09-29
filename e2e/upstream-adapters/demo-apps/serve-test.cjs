// pagelike's stand-in for demo-apps scripts/serve-test.cjs (docs/spec/apps.md
// §8.1). The upstream suite (tests/demo-pages.spec.js, run unchanged) calls
// startStaticServer(".deploy-ci", 0) and tests http://127.0.0.1:<port>; this
// module returns an http.Server on 127.0.0.1:<port> that reverse-proxies every
// request to the pagelike site named by PAGELIKE_DEMOS_ORIGIN, with the Host
// header pagelike routes by. server.address(), close() and
// closeAllConnections() keep their meaning.
const http = require("node:http");

function startStaticServer(_rootDirectory, port = 4173) {
  const target = new URL(process.env.PAGELIKE_DEMOS_ORIGIN);
  const server = http.createServer((req, res) => {
    const upstream = http.request(
      { host: "127.0.0.1", port: target.port, method: req.method, path: req.url, headers: { ...req.headers, host: target.host } },
      (up) => {
        res.writeHead(up.statusCode, up.headers);
        up.pipe(res);
      },
    );
    upstream.on("error", (err) => {
      res.writeHead(502, { "Content-Type": "text/plain" }).end(String(err));
    });
    req.pipe(upstream);
  });
  return new Promise((resolve, reject) => {
    server.once("error", reject);
    server.listen(port, "127.0.0.1", () => {
      server.off("error", reject);
      resolve(server);
    });
  });
}

module.exports = { startStaticServer };
