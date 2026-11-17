// Draws the disk image window again from macos/disk-image/background.html and
// layout.py: background.tiff (1× and 2×) and DS_Store (Finder's layout). Both
// are committed and copied into every image by pack-mac.mjs, so packing needs
// neither a browser nor Python. Run after changing either source, on a Mac
// with a Chromium-family browser (CHROME, else Google Chrome or Brave) and uv:
// `bun run draw:disk-image`.
import { execFile } from "node:child_process"
import { access, copyFile, mkdir, mkdtemp, rm } from "node:fs/promises"
import { tmpdir } from "node:os"
import { join } from "node:path"
import { promisify } from "node:util"

const exec = promisify(execFile)
const here = join(import.meta.dirname, "..", "macos", "disk-image")
const BROWSERS = [
  "/Applications/Google Chrome.app/Contents/MacOS/Google Chrome",
  "/Applications/Brave Browser.app/Contents/MacOS/Brave Browser",
]

async function browser() {
  if (process.env.CHROME) return process.env.CHROME
  for (const candidate of BROWSERS) {
    if (await access(candidate).then(() => true, () => false)) return candidate
  }
  throw new Error("No Chromium-family browser found: set CHROME to one.")
}

const work = await mkdtemp(join(tmpdir(), "wacli-disk-image-"))
const mount = join(work, "mnt")
try {
  const chrome = await browser()
  const shots = [1, 2].map(scale => join(work, `background@${scale}x.png`))
  for (const [index, shot] of shots.entries()) {
    await exec(chrome, [
      "--headless",
      "--disable-gpu",
      "--hide-scrollbars",
      `--user-data-dir=${join(work, "profile")}`,
      `--force-device-scale-factor=${index + 1}`,
      "--window-size=660,400",
      `--screenshot=${shot}`,
      `file://${join(here, "background.html")}`,
    ])
  }
  await exec("tiffutil", ["-cathidpicheck", ...shots, "-out", join(here, "background.tiff")])

  // dmgbuild only needs a folder named like the app to place its icon.
  await mkdir(join(work, "wacli.app"))
  const image = join(work, "layout.dmg")
  await exec("uv", [
    "run", "--quiet", "--with", "dmgbuild==1.6.7", "dmgbuild",
    "-s", join(here, "layout.py"),
    "-D", `app=${join(work, "wacli.app")}`,
    "-D", `background=${join(here, "background.tiff")}`,
    "wacli", image,
  ])
  await mkdir(mount)
  await exec("hdiutil", ["attach", "-quiet", "-nobrowse", "-readonly", "-mountpoint", mount, image])
  try {
    await copyFile(join(mount, ".DS_Store"), join(here, "DS_Store"))
  } finally {
    await exec("hdiutil", ["detach", "-quiet", mount])
  }
  console.log(`Drew ${join(here, "background.tiff")} and ${join(here, "DS_Store")}`)
} finally {
  await rm(work, { recursive: true, force: true })
}
