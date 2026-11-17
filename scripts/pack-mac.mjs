// Builds the Mac app and its disk image: dist/mac/wacli.app and
// dist/mac/wacli.dmg, for Apple Silicon and Intel Macs in one.
//
//   bun run pack:mac -- --dev    wacli.app only, signed for this Mac alone
//   bun run pack:mac             signed with the Developer ID, notarized,
//                                stapled, and put in the disk image
//
// The release build needs the Developer ID certificate in the keychain and an
// App Store Connect key for the notary in ASC_KEY_ID, ASC_ISSUER_ID and
// ASC_PRIVATE_KEY (the .p8 text). Nothing is uploaded anywhere but to Apple's
// notary, and nothing is published.
import { execFile } from "node:child_process"
import { rmSync } from "node:fs"
import { chmod, cp, mkdir, mkdtemp, readFile, rm, symlink, writeFile } from "node:fs/promises"
import { tmpdir } from "node:os"
import { join } from "node:path"
import { promisify } from "node:util"

const exec = promisify(execFile)
const root = join(import.meta.dirname, "..")
const source = join(root, "macos")
const out = join(root, "dist", "mac")
const app = join(out, "wacli.app")
const dmg = join(out, "wacli.dmg")

const IDENTITY = "Developer ID Application: Alive AI B.V. (N5CF4VM7JS)"
const MINIMUM_MACOS = "13.0"
const dev = process.argv.includes("--dev")

// The notary's key leaves the environment before any program is started, so
// the compilers and tools below never get it; notarytool reads it from a file.
const notary = { keyId: process.env.ASC_KEY_ID, issuer: process.env.ASC_ISSUER_ID, key: process.env.ASC_PRIVATE_KEY }
for (const name of ["ASC_KEY_ID", "ASC_ISSUER_ID", "ASC_PRIVATE_KEY"]) delete process.env[name]

async function run(command, args, options = {}) {
  try {
    return (await exec(command, args, { maxBuffer: 64 << 20, ...options })).stdout.trim()
  } catch (error) {
    throw new Error(`${command} ${args[0] ?? ""} failed:\n${error.stderr || error.stdout || error.message}`)
  }
}

// The version wacli itself prints, without the suffix a bundle cannot carry.
const sourceVersion = /sourceVersion = "([^"]+)"/.exec(await readFile(join(root, "cmd/wacli/root.go"), "utf8"))[1]
const version = sourceVersion.replace(/-.*/, "")
const build = await run("git", ["-C", root, "rev-list", "--count", "HEAD"])

await rm(out, { recursive: true, force: true })
const contents = join(app, "Contents")
for (const dir of ["MacOS", "Helpers", "Resources/en.lproj", "Resources/nl.lproj"]) {
  await mkdir(join(contents, dir), { recursive: true })
}

// wacli itself, once per kind of Mac, then one file for both.
const minimum = `-mmacosx-version-min=${MINIMUM_MACOS}`
const programs = []
for (const arch of ["arm64", "amd64"]) {
  const program = join(out, `wacli-${arch}`)
  await run("go", ["build", "-trimpath", "-tags", "sqlite_fts5", "-o", program, "./cmd/wacli"], {
    cwd: root,
    env: {
      ...process.env,
      GOOS: "darwin",
      GOARCH: arch,
      CGO_ENABLED: "1",
      CGO_CFLAGS: `${minimum} -Wno-error=missing-braces`,
      CGO_LDFLAGS: minimum,
    },
  })
  programs.push(program)
}
await run("lipo", ["-create", ...programs, "-output", join(contents, "Helpers", "wacli")])

// The app around it.
const swift = (await run("ls", [source])).split("\n").filter(name => name.endsWith(".swift") && name !== "icon.swift")
const shells = []
for (const arch of ["arm64", "x86_64"]) {
  const shell = join(out, `wacli-app-${arch}`)
  await run("xcrun", ["swiftc", "-O", "-target", `${arch}-apple-macos${MINIMUM_MACOS}`, "-o", shell, ...swift], { cwd: source })
  shells.push(shell)
}
await run("lipo", ["-create", ...shells, "-output", join(contents, "MacOS", "wacli-app")])
for (const file of [...programs, ...shells]) await rm(file)

const info = (await readFile(join(source, "Info.plist"), "utf8")).replace("VERSION", version).replace("BUILD", build)
await writeFile(join(contents, "Info.plist"), info)
await cp(join(source, "AppIcon.icns"), join(contents, "Resources", "AppIcon.icns"))

// Signing: the program inside first, then the app that seals it. Both run
// under the hardened runtime, which the notary requires.
const identity = dev ? "-" : IDENTITY
const sign = dev ? ["--force", "--sign", identity] : ["--force", "--options", "runtime", "--timestamp", "--sign", identity]
await run("codesign", [...sign, "--identifier", "site.alive.wacli.cli", join(contents, "Helpers", "wacli")])
await run("codesign", [...sign, app])
await run("codesign", ["--verify", "--deep", "--strict", app])

if (dev) {
  console.log(`Built ${app} (${sourceVersion}, build ${build}), signed for this Mac only.`)
  process.exit(0)
}

if (!notary.keyId || !notary.issuer || !notary.key) {
  throw new Error("ASC_KEY_ID, ASC_ISSUER_ID and ASC_PRIVATE_KEY must be set: the notary needs an App Store Connect key.")
}
const secrets = await mkdtemp(join(tmpdir(), "wacli-notary-"))
const keyFile = join(secrets, "key.p8")
// A build that is stopped while the notary works still takes the key file along.
for (const signal of ["SIGINT", "SIGTERM", "SIGHUP"]) {
  process.on(signal, () => {
    rmSync(secrets, { recursive: true, force: true })
    process.exit(1)
  })
}
await writeFile(keyFile, notary.key, { mode: 0o600 })
await chmod(keyFile, 0o600)
const notaryKey = ["--key", keyFile, "--key-id", notary.keyId, "--issuer", notary.issuer]

/** Sends one file to Apple's notary and waits for its answer. */
async function notarize(file) {
  const answer = JSON.parse(
    await run("xcrun", ["notarytool", "submit", file, "--wait", "--output-format", "json", ...notaryKey]),
  )
  if (answer.status !== "Accepted") {
    const log = await run("xcrun", ["notarytool", "log", answer.id, ...notaryKey]).catch(error => error.message)
    throw new Error(`The notary answered ${answer.status} for ${file}:\n${log}`)
  }
}

try {
  // The app first, so the ticket is stapled to the app that ends up in the image.
  const zip = join(out, "wacli.zip")
  await run("ditto", ["-c", "-k", "--keepParent", app, zip])
  await notarize(zip)
  await rm(zip)
  await run("xcrun", ["stapler", "staple", app])

  // The image: the app beside a link to Applications, in the drawn window.
  const stage = join(out, "disk-image")
  await mkdir(stage)
  await run("cp", ["-c", "-R", app, stage])
  await symlink("/Applications", join(stage, "Applications"))
  await cp(join(source, "disk-image", "background.tiff"), join(stage, ".background.tiff"))
  await cp(join(source, "disk-image", "DS_Store"), join(stage, ".DS_Store"))
  // Finder finds the background through the volume name.
  await run("hdiutil", ["create", "-volname", "wacli", "-srcfolder", stage, "-fs", "HFS+", "-format", "ULFO", "-quiet", "-ov", dmg])
  await rm(stage, { recursive: true })
  await run("codesign", ["--force", "--timestamp", "--sign", IDENTITY, dmg])
  await notarize(dmg)
  await run("xcrun", ["stapler", "staple", dmg])
} finally {
  await rm(secrets, { recursive: true, force: true })
}

// What a person who downloads it gets: Gatekeeper accepts the image and the app in it.
await run("xcrun", ["stapler", "validate", dmg])
await run("spctl", ["--assess", "--type", "open", "--context", "context:primary-signature", dmg])
const mount = await mkdtemp(join(tmpdir(), "wacli-disk-image-"))
await run("hdiutil", ["attach", dmg, "-readonly", "-nobrowse", "-mountpoint", mount, "-quiet"])
try {
  const mounted = join(mount, "wacli.app")
  await run("codesign", ["--verify", "--deep", "--strict", mounted])
  await run("xcrun", ["stapler", "validate", mounted])
  await run("spctl", ["--assess", "--type", "execute", mounted])
} finally {
  await run("hdiutil", ["detach", mount, "-force", "-quiet"])
  await rm(mount, { recursive: true, force: true })
}
console.log(`Built ${dmg} (${sourceVersion}, build ${build}): signed, notarized and stapled.`)
