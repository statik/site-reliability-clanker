// Draws the picture behind the icons in the window of the disk image: a dark band with
// the name, a light area for the two icons, an arrow from the app to Applications, and
// one small line with the version and the date of the release.
// scripts/macos/package.sh runs this file. The output is one TIFF file with the picture at
// 1x and at 2x, so Finder shows a sharp picture on each display.
//
// Finder writes the names of the icons in black, also in Dark Mode. Thus the area with
// the icons is light. The sizes agree with scripts/macos/dmg/settings.py. Change both.
import AppKit
import CoreGraphics
import Foundation

let width: CGFloat = 660
let height: CGFloat = 400
let band: CGFloat = 64
/// The centers of the icons, measured from the top left, as Finder does.
let appCenter = CGPoint(x: 170, y: 210)
let applicationsCenter = CGPoint(x: 490, y: 210)

enum Palette {
    static let honey: UInt32 = 0xF0B53F
    static let honeyDeep: UInt32 = 0xD98E2B
    static let cream: UInt32 = 0xF7ECD7
    static let seaDeep: UInt32 = 0x1B354A
}

func color(_ hex: UInt32, _ alpha: CGFloat = 1) -> CGColor {
    CGColor(
        red: CGFloat((hex >> 16) & 0xFF) / 255, green: CGFloat((hex >> 8) & 0xFF) / 255,
        blue: CGFloat(hex & 0xFF) / 255, alpha: alpha
    )
}

guard let space = CGColorSpace(name: CGColorSpace.sRGB) else {
    fatalError("dmg-background: the sRGB color space is not available")
}

/// Core Graphics measures y from the bottom. The layout is given from the top.
func fromTop(_ point: CGPoint) -> CGPoint {
    CGPoint(x: point.x, y: height - point.y)
}

func drawText(
    _ text: String, size: CGFloat, weight: NSFont.Weight, hex: UInt32, centerY: CGFloat,
    in context: CGContext
) {
    let line = NSAttributedString(string: text, attributes: [
        .font: NSFont.systemFont(ofSize: size, weight: weight),
        .foregroundColor: NSColor(cgColor: color(hex)) ?? NSColor.black,
        .kern: 0.3,
    ])
    let bounds = line.size()
    NSGraphicsContext.saveGraphicsState()
    NSGraphicsContext.current = NSGraphicsContext(cgContext: context, flipped: false)
    line.draw(at: CGPoint(x: (width - bounds.width) / 2, y: height - centerY - bounds.height / 2))
    NSGraphicsContext.restoreGraphicsState()
}

func drawGround(in context: CGContext) {
    context.setFillColor(color(Palette.cream))
    context.fill(CGRect(x: 0, y: 0, width: width, height: height))
    context.setFillColor(color(Palette.seaDeep))
    context.fill(CGRect(x: 0, y: height - band, width: width, height: band))
    context.setFillColor(color(Palette.honey))
    context.fill(CGRect(x: 0, y: height - band - 4, width: width, height: 4))
}

/// A line of dashes from the app to Applications, with an arrowhead.
func drawArrow(in context: CGContext) {
    let start = fromTop(CGPoint(x: appCenter.x + 96, y: appCenter.y - 10))
    let end = fromTop(CGPoint(x: applicationsCenter.x - 96, y: applicationsCenter.y - 10))
    context.setStrokeColor(color(Palette.honeyDeep))
    context.setLineWidth(5)
    context.setLineCap(.round)
    context.setLineJoin(.round)
    context.setLineDash(phase: 0, lengths: [10, 12])
    context.move(to: start)
    context.addLine(to: CGPoint(x: end.x - 6, y: end.y))
    context.strokePath()
    context.setLineDash(phase: 0, lengths: [])
    context.move(to: CGPoint(x: end.x - 16, y: end.y + 14))
    context.addLine(to: end)
    context.addLine(to: CGPoint(x: end.x - 16, y: end.y - 14))
    context.strokePath()
}

func render(scale: Int, label: String) -> CGImage {
    guard let context = CGContext(
        data: nil, width: Int(width) * scale, height: Int(height) * scale, bitsPerComponent: 8,
        bytesPerRow: 0, space: space, bitmapInfo: CGImageAlphaInfo.premultipliedLast.rawValue
    ) else {
        fatalError("dmg-background: could not make a bitmap at \(scale)x")
    }
    context.scaleBy(x: CGFloat(scale), y: CGFloat(scale))
    drawGround(in: context)
    drawText(
        "S.R.C.  Site Reliability Clanker", size: 20, weight: .semibold, hex: Palette.cream,
        centerY: band / 2, in: context
    )
    drawArrow(in: context)
    drawText(
        "Drag the app to Applications. Then open it there.", size: 14, weight: .medium,
        hex: Palette.seaDeep, centerY: 340, in: context
    )
    drawText(label, size: 11, weight: .regular, hex: Palette.seaDeep, centerY: 376, in: context)
    guard let image = context.makeImage() else {
        fatalError("dmg-background: could not render at \(scale)x")
    }
    return image
}

guard CommandLine.arguments.count == 3 else {
    FileHandle.standardError.write(Data("usage: dmg-background <out.tiff> <label>\n".utf8))
    exit(2)
}
let picture = NSImage(size: NSSize(width: width, height: height))
for scale in [1, 2] {
    let rep = NSBitmapImageRep(cgImage: render(scale: scale, label: CommandLine.arguments[2]))
    rep.size = NSSize(width: width, height: height)
    picture.addRepresentation(rep)
}
guard let data = picture.tiffRepresentation(using: .lzw, factor: 0) else {
    fatalError("dmg-background: could not encode the TIFF file")
}
let output = URL(fileURLWithPath: CommandLine.arguments[1])
try data.write(to: output)
print("dmg-background: wrote \(output.path)")
