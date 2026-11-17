import SwiftUI

/// The mark of S.R.C.: a person who directs aircraft with two wands. The drawing is the one
/// of `site/img/icon.svg`, in the same 64-unit grid.
struct ProductMark: View {
  var size: CGFloat = 64

  var body: some View {
    let unit = size / MarkGeometry.grid
    ZStack {
      RoundedRectangle(cornerRadius: 10 * unit)
        .fill(Palette.markGround)
      MarkWands()
        .fill(Palette.honey)
      MarkWands()
        .stroke(Palette.honey, style: StrokeStyle(lineWidth: 2 * unit, lineJoin: .round))
      MarkArms()
        .stroke(Palette.cream, style: StrokeStyle(lineWidth: 6 * unit, lineCap: .round))
      MarkBody()
        .fill(Palette.cream)
    }
    .frame(width: size, height: size)
    .accessibilityHidden(true)
  }
}

private enum MarkGeometry {
  static let grid: CGFloat = 64

  /// Scales a path that was drawn in the 64-unit grid to the rectangle of the shape.
  static func fit(_ path: Path, in rect: CGRect) -> Path {
    path.applying(
      CGAffineTransform(translationX: rect.minX, y: rect.minY)
        .scaledBy(x: rect.width / grid, y: rect.height / grid))
  }
}

private struct MarkWands: Shape {
  func path(in rect: CGRect) -> Path {
    var path = Path()
    path.addLines([
      CGPoint(x: 6.5, y: 22), CGPoint(x: 16.5, y: 19.6), CGPoint(x: 9.4, y: 2.8),
      CGPoint(x: 7.6, y: 3.2),
    ])
    path.closeSubpath()
    path.addLines([
      CGPoint(x: 57.5, y: 22), CGPoint(x: 47.5, y: 19.6), CGPoint(x: 54.6, y: 2.8),
      CGPoint(x: 56.4, y: 3.2),
    ])
    path.closeSubpath()
    return MarkGeometry.fit(path, in: rect)
  }
}

private struct MarkArms: Shape {
  func path(in rect: CGRect) -> Path {
    var path = Path()
    path.move(to: CGPoint(x: 25, y: 31))
    path.addLine(to: CGPoint(x: 12, y: 23.5))
    path.move(to: CGPoint(x: 39, y: 31))
    path.addLine(to: CGPoint(x: 52, y: 23.5))
    return MarkGeometry.fit(path, in: rect)
  }
}

private struct MarkBody: Shape {
  func path(in rect: CGRect) -> Path {
    let torso = UnevenRoundedRectangle(topLeadingRadius: 4, topTrailingRadius: 4)
    let leg = UnevenRoundedRectangle(bottomLeadingRadius: 2, bottomTrailingRadius: 2)
    var path = Path()
    path.addEllipse(in: CGRect(x: 25.5, y: 10.5, width: 13, height: 13))
    path.addPath(torso.path(in: CGRect(x: 23.5, y: 26, width: 17, height: 19)))
    path.addPath(leg.path(in: CGRect(x: 23.5, y: 43, width: 7.5, height: 17)))
    path.addPath(leg.path(in: CGRect(x: 33, y: 43, width: 7.5, height: 17)))
    return MarkGeometry.fit(path, in: rect)
  }
}
