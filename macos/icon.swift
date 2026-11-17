// Draws the app icon: `swift macos/icon.swift macos/AppIcon.icns`.
// A silver tile, a dark speech bubble, and the green dot of the pairing page.
import AppKit

let side: CGFloat = 1024
let image = NSImage(size: NSSize(width: side, height: side), flipped: false) { _ in
    let context = NSGraphicsContext.current!.cgContext

    // The tile of a macOS icon: 824 pt in a 1024 pt canvas, with its own shadow.
    let tile = NSRect(x: 100, y: 100, width: 824, height: 824)
    let shape = NSBezierPath(roundedRect: tile, xRadius: 186, yRadius: 186)
    context.saveGState()
    context.setShadow(offset: CGSize(width: 0, height: -12), blur: 28, color: NSColor.black.withAlphaComponent(0.28).cgColor)
    NSColor.white.setFill()
    shape.fill()
    context.restoreGState()
    NSGradient(starting: .white, ending: NSColor(srgbRed: 0xE2 / 255, green: 0xE2 / 255, blue: 0xE6 / 255, alpha: 1))!
        .draw(in: shape, angle: -90)

    // The bubble, with its tail at the lower left.
    let ink = NSColor(srgbRed: 0x1B / 255, green: 0x1B / 255, blue: 0x1E / 255, alpha: 1)
    let bubble = NSRect(x: 252, y: 372, width: 520, height: 400)
    let body = NSBezierPath(roundedRect: bubble, xRadius: 132, yRadius: 132)
    let tail = NSBezierPath()
    tail.move(to: NSPoint(x: 330, y: 420))
    tail.line(to: NSPoint(x: 312, y: 288))
    tail.line(to: NSPoint(x: 470, y: 380))
    tail.close()
    tail.lineJoinStyle = .round
    ink.setFill()
    ink.setStroke()
    body.fill()
    tail.lineWidth = 28
    tail.fill()
    tail.stroke()

    NSColor(srgbRed: 0, green: 192 / 255, blue: 96 / 255, alpha: 1).setFill()
    NSBezierPath(ovalIn: NSRect(x: bubble.midX - 76, y: bubble.midY - 76, width: 152, height: 152)).fill()
    return true
}

func png(_ pixels: Int) -> Data {
    let bitmap = NSBitmapImageRep(bitmapDataPlanes: nil, pixelsWide: pixels, pixelsHigh: pixels, bitsPerSample: 8,
                                  samplesPerPixel: 4, hasAlpha: true, isPlanar: false, colorSpaceName: .deviceRGB,
                                  bytesPerRow: 0, bitsPerPixel: 0)!
    bitmap.size = NSSize(width: pixels, height: pixels)
    NSGraphicsContext.saveGraphicsState()
    NSGraphicsContext.current = NSGraphicsContext(bitmapImageRep: bitmap)
    image.draw(in: NSRect(x: 0, y: 0, width: pixels, height: pixels))
    NSGraphicsContext.restoreGraphicsState()
    return bitmap.representation(using: .png, properties: [:])!
}

let out = URL(fileURLWithPath: CommandLine.arguments[1])
let iconset = FileManager.default.temporaryDirectory.appendingPathComponent("wacli-\(ProcessInfo.processInfo.processIdentifier).iconset")
try FileManager.default.createDirectory(at: iconset, withIntermediateDirectories: true)
for points in [16, 32, 128, 256, 512] {
    try png(points).write(to: iconset.appendingPathComponent("icon_\(points)x\(points).png"))
    try png(points * 2).write(to: iconset.appendingPathComponent("icon_\(points)x\(points)@2x.png"))
}
let iconutil = Process()
iconutil.executableURL = URL(fileURLWithPath: "/usr/bin/iconutil")
iconutil.arguments = ["-c", "icns", "-o", out.path, iconset.path]
try iconutil.run()
iconutil.waitUntilExit()
try FileManager.default.removeItem(at: iconset)
exit(iconutil.terminationStatus)
