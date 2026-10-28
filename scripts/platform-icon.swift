// Renders a platform's app icon from the square artwork in resources/icon.png:
// the artwork clipped to a rounded tile with a soft shadow, on a transparent
// 1024 canvas. Neither macOS (before 26) nor Windows draws a mask of its own,
// so the release builds swap these in for resources/icon.png. The two differ
// only in the tile's size: macOS keeps the 824 pixel tile of its icon grid,
// Windows fills more of the canvas.
//
//   swift scripts/platform-icon.swift resources/icon.png packaging/icon-macos.png 824
//   swift scripts/platform-icon.swift resources/icon.png packaging/icon-windows.png 944
import AppKit

let canvas = 1024
let exponent = 5.0 // superellipse exponent, close to the macOS continuous corner

func squircle(in rect: CGRect) -> CGPath {
    let path = CGMutablePath()
    let steps = 720
    let a = rect.width / 2, b = rect.height / 2
    for i in 0...steps {
        let t = Double(i) / Double(steps) * 2 * Double.pi
        let c = cos(t), s = sin(t)
        let x = rect.midX + a * (c < 0 ? -1 : 1) * pow(abs(c), 2 / exponent)
        let y = rect.midY + b * (s < 0 ? -1 : 1) * pow(abs(s), 2 / exponent)
        if i == 0 { path.move(to: CGPoint(x: x, y: y)) } else { path.addLine(to: CGPoint(x: x, y: y)) }
    }
    path.closeSubpath()
    return path
}

let args = CommandLine.arguments
guard args.count == 4, let tile = Double(args[3]), tile > 0, tile <= Double(canvas),
      let art = NSImage(contentsOfFile: args[1])?.cgImage(forProposedRect: nil, context: nil, hints: nil) else {
    FileHandle.standardError.write(Data("usage: platform-icon.swift <icon.png> <out.png> <tile size, up to 1024>\n".utf8))
    exit(1)
}
guard let ctx = CGContext(data: nil, width: canvas, height: canvas, bitsPerComponent: 8, bytesPerRow: 0,
                          space: CGColorSpace(name: CGColorSpace.sRGB)!,
                          bitmapInfo: CGImageAlphaInfo.premultipliedLast.rawValue) else { exit(1) }
let rect = CGRect(x: (Double(canvas) - tile) / 2, y: (Double(canvas) - tile) / 2, width: tile, height: tile)
let path = squircle(in: rect)
// The shadow scales with the tile, so both platforms look alike.
let scale = tile / 824.0

ctx.saveGState()
ctx.setShadow(offset: CGSize(width: 0, height: -10 * scale), blur: 24 * scale, color: CGColor(gray: 0, alpha: 0.28))
ctx.addPath(path)
ctx.setFillColor(CGColor(gray: 1, alpha: 1))
ctx.fillPath()
ctx.restoreGState()

ctx.saveGState()
ctx.addPath(path)
ctx.clip()
ctx.interpolationQuality = .high
ctx.draw(art, in: rect)
ctx.restoreGState()

guard let out = ctx.makeImage(),
      let png = NSBitmapImageRep(cgImage: out).representation(using: .png, properties: [:]) else { exit(1) }
try png.write(to: URL(fileURLWithPath: args[2]))
