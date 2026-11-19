// Draws the app icon: the marshaller with two wands of site/img/icon.svg, on the rounded
// square that macOS icons use. scripts/macos/package.sh runs this file. It writes the
// PNG files of an .iconset folder, and `iconutil` makes the .icns file from them.
//
// The shapes use the 64-unit grid of the SVG file, with y down. Change both together.
import AppKit
import CoreGraphics
import Foundation

enum Palette {
    static let honey: UInt32 = 0xF0B53F
    static let cream: UInt32 = 0xF7ECD7
    static let seaMid: UInt32 = 0x24465F
    static let seaDeep: UInt32 = 0x1B354A
}

func color(_ hex: UInt32, _ alpha: CGFloat = 1) -> CGColor {
    CGColor(
        red: CGFloat((hex >> 16) & 0xFF) / 255, green: CGFloat((hex >> 8) & 0xFF) / 255,
        blue: CGFloat(hex & 0xFF) / 255, alpha: alpha
    )
}

guard let space = CGColorSpace(name: CGColorSpace.sRGB) else {
    fatalError("icon: the sRGB color space is not available")
}

/// A rectangle with a radius for the two top corners and one for the two bottom corners.
func block(_ rect: CGRect, top: CGFloat, bottom: CGFloat) -> CGPath {
    let path = CGMutablePath()
    path.move(to: CGPoint(x: rect.minX, y: rect.minY + top))
    path.addArc(
        tangent1End: CGPoint(x: rect.minX, y: rect.minY),
        tangent2End: CGPoint(x: rect.minX + top, y: rect.minY), radius: top
    )
    path.addArc(
        tangent1End: CGPoint(x: rect.maxX, y: rect.minY),
        tangent2End: CGPoint(x: rect.maxX, y: rect.minY + top), radius: top
    )
    path.addArc(
        tangent1End: CGPoint(x: rect.maxX, y: rect.maxY),
        tangent2End: CGPoint(x: rect.maxX - bottom, y: rect.maxY), radius: bottom
    )
    path.addArc(
        tangent1End: CGPoint(x: rect.minX, y: rect.maxY),
        tangent2End: CGPoint(x: rect.minX, y: rect.maxY - bottom), radius: bottom
    )
    path.closeSubpath()
    return path
}

func polygon(_ points: [(CGFloat, CGFloat)]) -> CGPath {
    let path = CGMutablePath()
    path.addLines(between: points.map { CGPoint(x: $0.0, y: $0.1) })
    path.closeSubpath()
    return path
}

/// The two wands, in the honey color.
func drawWands(in context: CGContext) {
    context.setFillColor(color(Palette.honey))
    context.setStrokeColor(color(Palette.honey))
    context.setLineWidth(2)
    context.setLineJoin(.round)
    for wand in [
        polygon([(6.5, 22), (16.5, 19.6), (9.4, 2.8), (7.6, 3.2)]),
        polygon([(57.5, 22), (47.5, 19.6), (54.6, 2.8), (56.4, 3.2)]),
    ] {
        context.addPath(wand)
        context.drawPath(using: .fillStroke)
    }
}

/// The arms, the head, the body and the legs, in the cream color.
func drawPerson(in context: CGContext) {
    context.setStrokeColor(color(Palette.cream))
    context.setLineWidth(6)
    context.setLineCap(.round)
    for (from, to) in [((25.0, 31.0), (12.0, 23.5)), ((39.0, 31.0), (52.0, 23.5))] {
        context.move(to: CGPoint(x: from.0, y: from.1))
        context.addLine(to: CGPoint(x: to.0, y: to.1))
    }
    context.strokePath()
    context.setFillColor(color(Palette.cream))
    context.fillEllipse(in: CGRect(x: 25.5, y: 10.5, width: 13, height: 13))
    context.addPath(block(CGRect(x: 23.5, y: 26, width: 17, height: 19), top: 4, bottom: 0))
    context.addPath(block(CGRect(x: 23.5, y: 43, width: 7.5, height: 17), top: 0, bottom: 2))
    context.addPath(block(CGRect(x: 33, y: 43, width: 7.5, height: 17), top: 0, bottom: 2))
    context.fillPath()
}

func render(pixels: Int) -> CGImage {
    guard let context = CGContext(
        data: nil, width: pixels, height: pixels, bitsPerComponent: 8, bytesPerRow: 0,
        space: space, bitmapInfo: CGImageAlphaInfo.premultipliedLast.rawValue
    ) else {
        fatalError("icon: could not make a bitmap of \(pixels) pixels")
    }
    // A macOS icon is a rounded square of 824 points on a canvas of 1024 points.
    let unit = CGFloat(pixels) / 1024
    let tile = CGRect(x: 100 * unit, y: 100 * unit, width: 824 * unit, height: 824 * unit)
    let shape = CGPath(
        roundedRect: tile, cornerWidth: 185 * unit, cornerHeight: 185 * unit, transform: nil
    )
    context.setShadow(
        offset: CGSize(width: 0, height: -10 * unit), blur: 24 * unit,
        color: CGColor(gray: 0, alpha: 0.3)
    )
    context.addPath(shape)
    context.setFillColor(color(Palette.seaDeep))
    context.fillPath()
    context.setShadow(offset: .zero, blur: 0, color: nil)

    context.saveGState()
    context.addPath(shape)
    context.clip()
    guard let fade = CGGradient(
        colorsSpace: space,
        colors: [color(Palette.seaMid), color(Palette.seaDeep)] as CFArray, locations: [0, 1]
    ) else {
        fatalError("icon: could not make the gradient")
    }
    context.drawLinearGradient(
        fade, start: CGPoint(x: 0, y: tile.maxY), end: CGPoint(x: 0, y: tile.minY), options: []
    )
    // The grid of the SVG file, with y down, a little smaller than the tile.
    let inset = tile.insetBy(dx: 36 * unit, dy: 36 * unit)
    context.translateBy(x: inset.minX, y: inset.maxY)
    context.scaleBy(x: inset.width / 64, y: -inset.height / 64)
    drawWands(in: context)
    drawPerson(in: context)
    context.restoreGState()

    guard let image = context.makeImage() else {
        fatalError("icon: could not render \(pixels) pixels")
    }
    return image
}

guard CommandLine.arguments.count == 2 else {
    FileHandle.standardError.write(Data("usage: icon <out.iconset>\n".utf8))
    exit(2)
}
let folder = URL(fileURLWithPath: CommandLine.arguments[1])
try FileManager.default.createDirectory(at: folder, withIntermediateDirectories: true)
for (points, scale) in [(16, 1), (16, 2), (32, 1), (32, 2), (128, 1), (128, 2), (256, 1),
                        (256, 2), (512, 1), (512, 2)]
{
    let rep = NSBitmapImageRep(cgImage: render(pixels: points * scale))
    guard let png = rep.representation(using: .png, properties: [:]) else {
        fatalError("icon: could not encode \(points) points at \(scale)x")
    }
    let suffix = scale == 2 ? "@2x" : ""
    try png.write(to: folder.appendingPathComponent("icon_\(points)x\(points)\(suffix).png"))
}
print("icon: wrote \(folder.path)")
