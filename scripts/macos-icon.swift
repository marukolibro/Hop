// Renders packaging/icon-macos.png from resources/icon.png: the artwork clipped
// to a rounded tile with a soft shadow, on a transparent 1024 canvas, following
// the macOS icon grid (an 824 pixel tile centred in the canvas). macOS draws no
// mask of its own, so the release build swaps this in for resources/icon.png.
//
//   swift scripts/macos-icon.swift resources/icon.png packaging/icon-macos.png
import AppKit

let canvas = 1024
let tile = 824.0
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
guard args.count == 3, let art = NSImage(contentsOfFile: args[1])?.cgImage(forProposedRect: nil, context: nil, hints: nil) else {
    FileHandle.standardError.write(Data("usage: macos-icon.swift <icon.png> <out.png>\n".utf8))
    exit(1)
}
guard let ctx = CGContext(data: nil, width: canvas, height: canvas, bitsPerComponent: 8, bytesPerRow: 0,
                          space: CGColorSpace(name: CGColorSpace.sRGB)!,
                          bitmapInfo: CGImageAlphaInfo.premultipliedLast.rawValue) else { exit(1) }
let rect = CGRect(x: (Double(canvas) - tile) / 2, y: (Double(canvas) - tile) / 2, width: tile, height: tile)
let path = squircle(in: rect)

ctx.saveGState()
ctx.setShadow(offset: CGSize(width: 0, height: -10), blur: 24, color: CGColor(gray: 0, alpha: 0.28))
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
