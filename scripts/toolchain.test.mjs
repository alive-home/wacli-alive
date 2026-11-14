import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import test from "node:test";

const read = (file) => readFileSync(new URL(`../${file}`, import.meta.url), "utf8");

test("every build path uses the Go toolchain go.mod selects, without raising the source minimum", () => {
  const module = read("go.mod");
  assert.equal(module.match(/^go (\S+)$/m)[1], "1.27.0");
  const version = module.match(/^toolchain go(\S+)$/m)[1];
  assert.equal(read("Makefile").match(/GOVERSION\)" = (go\S+)/)[1], `go${version}`);
  assert.equal(read("Dockerfile").match(/FROM golang:([\d.]+)-alpine@/)[1], version);
});
