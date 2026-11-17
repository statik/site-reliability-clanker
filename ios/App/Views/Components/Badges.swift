import SwiftUI

extension Tone {
  var color: PaletteColor {
    switch self {
    case .good: Palette.success
    case .attention: Palette.warning
    case .bad: Palette.error
    case .active: Palette.active
    case .neutral: Palette.textSecondary
    }
  }
}

extension RiskTier {
  /// The ground of the tier tag, as the website colours the tiers. The text on it is
  /// `Palette.tagText`.
  var tagBackground: PaletteColor {
    switch self {
    case .t0: Palette.tagGray
    case .t1: Palette.tagSea
    case .t2: Palette.tagSky
    case .t3: Palette.tagRed
    case .none, .unknown: Palette.tagGray
    }
  }
}

/// Two views side by side, or one above the other when the text size leaves no room.
struct AdaptiveRow<Leading: View, Trailing: View>: View {
  @ViewBuilder let leading: Leading
  @ViewBuilder let trailing: Trailing

  var body: some View {
    ViewThatFits(in: .horizontal) {
      HStack(alignment: .firstTextBaseline) {
        leading.lineLimit(1)
        Spacer(minLength: 8)
        trailing.lineLimit(1)
      }
      VStack(alignment: .leading, spacing: 6) {
        leading
        trailing
      }
      .frame(maxWidth: .infinity, alignment: .leading)
    }
  }
}

/// A status as icon and word. Colour supports the two; it never carries the meaning alone.
struct StatusLabel: View {
  let text: String
  let symbol: String
  let tone: Tone

  var body: some View {
    HStack(alignment: .firstTextBaseline, spacing: 6) {
      Image(systemName: symbol)
        .accessibilityHidden(true)
      Text(text)
    }
    .font(.subheadline.weight(.medium))
    .foregroundStyle(tone.color)
    .accessibilityElement(children: .combine)
  }
}

extension StatusLabel {
  init(state: WorkState) {
    self.init(text: state.label, symbol: state.symbol, tone: state.tone)
  }
}

/// The risk tier as a capsule: code, icon and meaning.
struct TierBadge: View {
  let tier: RiskTier
  var showsMeaning = false

  var body: some View {
    HStack(spacing: 4) {
      Image(systemName: tier.symbol)
        .imageScale(.small)
      Text(showsMeaning ? "\(tier.code) · \(tier.meaning)" : tier.code)
        .fontWeight(.semibold)
    }
    .font(.caption)
    .padding(.horizontal, 8)
    .padding(.vertical, 3)
    .foregroundStyle(Palette.tagText)
    .background(tier.tagBackground, in: RoundedRectangle(cornerRadius: 10))
    .accessibilityElement(children: .ignore)
    .accessibilityLabel("Risk tier \(tier.code), \(tier.meaning)")
  }
}

/// Says where a piece of text comes from: the model, a template or the plan.
struct SourceNote: View {
  enum Origin {
    case model(source: String)
    case plan
    case policy
    case auditLog

    var symbol: String {
      switch self {
      case .model: "sparkles"
      case .plan: "doc.text.magnifyingglass"
      case .policy: "checkmark.shield"
      case .auditLog: "link"
      }
    }

    var text: String {
      switch self {
      case .model(let source): Self.modelText(source)
      case .plan: "From the saved plan, not from the model"
      case .policy: "Set by policy code, not by the model"
      case .auditLog: "From the audit log"
      }
    }

    private static func modelText(_ source: String) -> String {
      switch source {
      case "": "AI-written explanation. Source not stated"
      case "template": "Written from a fixed template, not by a model"
      case "replay": "AI-written (replayed recording)"
      default: "AI-written by \(source). Check it against the plan facts"
      }
    }
  }

  let origin: Origin

  var body: some View {
    Label(origin.text, systemImage: origin.symbol)
      .font(.caption)
      .foregroundStyle(Palette.textSecondary)
      .accessibilityElement(children: .combine)
  }
}

/// A row with a small caption above a value that may wrap over several lines.
struct FieldRow: View {
  let label: String
  let value: String
  var monospaced = false

  var body: some View {
    VStack(alignment: .leading, spacing: 2) {
      Text(label)
        .font(.caption)
        .foregroundStyle(Palette.textSecondary)
      Text(value.isEmpty ? "Not stated" : value)
        .font(monospaced ? .callout.monospaced() : .callout)
        .foregroundStyle(value.isEmpty ? Palette.textSecondary : Palette.text)
        .textSelection(.enabled)
    }
    .frame(maxWidth: .infinity, alignment: .leading)
    .accessibilityElement(children: .combine)
  }
}

/// An error from a server or the network, shown where the data would be.
struct ErrorNotice: View {
  let message: String
  var status: String?
  var retry: (() -> Void)?

  var body: some View {
    VStack(alignment: .leading, spacing: 8) {
      Label(status ?? "Something went wrong", systemImage: "exclamationmark.triangle.fill")
        .font(.subheadline.weight(.semibold))
        .foregroundStyle(Palette.error)
      Text(message)
        .font(.callout)
        .textSelection(.enabled)
      if let retry {
        Button("Try again", action: retry)
          .buttonStyle(PaletteBoxButtonStyle())
      }
    }
    .frame(maxWidth: .infinity, alignment: .leading)
    .accessibilityElement(children: .contain)
    .accessibilityIdentifier("error-notice")
  }
}
