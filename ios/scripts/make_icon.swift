// Draws the app icon: a folded paper boat on water with one fish below it.
// Run from ios/: swift scripts/make_icon.swift App/Assets.xcassets/AppIcon.appiconset/AppIcon.png
import CoreGraphics
import Foundation
import ImageIO
import UniformTypeIdentifiers

let size = 1024
let space = CGColorSpaceCreateDeviceRGB()
guard
  let ctx = CGContext(
    data: nil, width: size, height: size, bitsPerComponent: 8, bytesPerRow: 0, space: space,
    bitmapInfo: CGImageAlphaInfo.noneSkipLast.rawValue)
else { fatalError("cannot create the bitmap context") }

func color(_ red: CGFloat, _ green: CGFloat, _ blue: CGFloat) -> CGColor {
  CGColor(colorSpace: space, components: [red, green, blue, 1]) ?? CGColor(gray: 0, alpha: 1)
}

func fill(_ points: [(CGFloat, CGFloat)], _ fillColor: CGColor) {
  guard let first = points.first else { return }
  ctx.beginPath()
  ctx.move(to: CGPoint(x: first.0, y: first.1))
  for point in points.dropFirst() { ctx.addLine(to: CGPoint(x: point.0, y: point.1)) }
  ctx.closePath()
  ctx.setFillColor(fillColor)
  ctx.fillPath()
}

func wave(baseline: CGFloat, amplitude: CGFloat, phase: CGFloat, _ fillColor: CGColor) {
  ctx.beginPath()
  ctx.move(to: CGPoint(x: 0, y: 0))
  ctx.addLine(to: CGPoint(x: 0, y: baseline))
  var x: CGFloat = 0
  while x <= 1024 {
    let y = baseline + amplitude * sin(x / 1024 * 4 * .pi + phase)
    ctx.addLine(to: CGPoint(x: x, y: y))
    x += 8
  }
  ctx.addLine(to: CGPoint(x: 1024, y: 0))
  ctx.closePath()
  ctx.setFillColor(fillColor)
  ctx.fillPath()
}

let sky = [color(0.07, 0.38, 0.58), color(0.04, 0.24, 0.42)] as CFArray
if let gradient = CGGradient(colorsSpace: space, colors: sky, locations: [0, 1]) {
  ctx.drawLinearGradient(
    gradient, start: CGPoint(x: 0, y: 1024), end: CGPoint(x: 0, y: 0), options: [])
}

let paper = color(0.98, 0.97, 0.94)
let shade = color(0.84, 0.87, 0.88)
let fold = color(0.68, 0.73, 0.76)
fill([(512, 820), (512, 450), (330, 450)], paper)
fill([(512, 820), (512, 450), (694, 450)], shade)
fill([(150, 500), (512, 500), (512, 300), (320, 300)], paper)
fill([(874, 500), (512, 500), (512, 300), (704, 300)], shade)
fill([(330, 450), (694, 450), (512, 500)], fold)

wave(baseline: 330, amplitude: 22, phase: 0, color(0.17, 0.58, 0.74))
wave(baseline: 250, amplitude: 18, phase: 1.6, color(0.10, 0.45, 0.64))

let fish = color(0.99, 0.74, 0.38)
ctx.setFillColor(fish)
ctx.fillEllipse(in: CGRect(x: 600, y: 96, width: 160, height: 74))
fill([(612, 133), (536, 182), (536, 84)], fish)
ctx.setFillColor(color(0.10, 0.30, 0.45))
ctx.fillEllipse(in: CGRect(x: 716, y: 136, width: 14, height: 14))

guard let image = ctx.makeImage() else { fatalError("cannot make the image") }
let out = URL(fileURLWithPath: CommandLine.arguments.dropFirst().first ?? "AppIcon.png")
guard
  let destination = CGImageDestinationCreateWithURL(
    out as CFURL, UTType.png.identifier as CFString, 1, nil)
else { fatalError("cannot open \(out.path) for writing") }
CGImageDestinationAddImage(destination, image, nil)
guard CGImageDestinationFinalize(destination) else { fatalError("cannot write \(out.path)") }
