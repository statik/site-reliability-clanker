import SwiftUI

/// What a list shows when it has nothing to show.
struct EmptyState: View {
  let title: String
  let message: String

  var body: some View {
    VStack(spacing: 12) {
      ProductMark()
      Text(title)
        .font(.headline)
      Text(message)
        .font(.subheadline)
        .foregroundStyle(Palette.textSecondary)
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
          .foregroundStyle(Palette.textSecondary)
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
          .foregroundStyle(Palette.textSecondary)
      }
    }
    .padding(.vertical, 2)
    .accessibilityElement(children: .combine)
    .accessibilityIdentifier("work-item-\(item.id)")
  }
}
