import SwiftUI

/// The outline of a folded paper boat: one sail and a hull.
struct PaperBoatShape: Shape {
  func path(in rect: CGRect) -> Path {
    func point(_ x: CGFloat, _ y: CGFloat) -> CGPoint {
      CGPoint(x: rect.minX + x * rect.width, y: rect.minY + y * rect.height)
    }
    var path = Path()
    path.move(to: point(0.5, 0.05))
    path.addLine(to: point(0.72, 0.55))
    path.addLine(to: point(0.28, 0.55))
    path.closeSubpath()
    path.move(to: point(0.04, 0.55))
    path.addLine(to: point(0.96, 0.55))
    path.addLine(to: point(0.76, 0.9))
    path.addLine(to: point(0.24, 0.9))
    path.closeSubpath()
    return path
  }
}

/// A small paper boat on a line of water, for empty states.
struct PaperBoatMark: View {
  var size: CGFloat = 72

  var body: some View {
    VStack(spacing: 2) {
      PaperBoatShape()
        .fill(.tint.opacity(0.85))
        .frame(width: size, height: size * 0.8)
      Image(systemName: "water.waves")
        .font(.system(size: size * 0.45))
        .foregroundStyle(.tint.opacity(0.5))
    }
    .accessibilityHidden(true)
  }
}

/// What a list shows when it has nothing to show.
struct EmptyState: View {
  let title: String
  let message: String

  var body: some View {
    VStack(spacing: 12) {
      PaperBoatMark()
      Text(title)
        .font(.headline)
      Text(message)
        .font(.subheadline)
        .foregroundStyle(.secondary)
        .multilineTextAlignment(.center)
    }
    .frame(maxWidth: .infinity)
    .padding(.vertical, 28)
    .accessibilityElement(children: .combine)
  }
}

/// One work item in a list: title, state, tier and where it applies.
struct WorkItemRow: View {
  let item: WorkItem

  var body: some View {
    VStack(alignment: .leading, spacing: 6) {
      HStack(alignment: .firstTextBaseline) {
        Text(item.id)
          .font(.caption.monospaced())
          .foregroundStyle(.secondary)
        Spacer()
        if item.riskTier != .none {
          TierBadge(tier: item.riskTier)
        }
      }
      Text(item.title)
        .font(.body.weight(.medium))
      AdaptiveRow {
        StatusLabel(state: item.state)
      } trailing: {
        Text("\(Humanise.kind(item.kind)) · \(item.env)")
          .font(.caption)
          .foregroundStyle(.secondary)
      }
    }
    .padding(.vertical, 2)
    .accessibilityElement(children: .combine)
    .accessibilityIdentifier("work-item-\(item.id)")
  }
}
