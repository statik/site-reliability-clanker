import Foundation
import Testing

@testable import PaperboatManager

/// The contrast ratio of WCAG 2 between two colours, from 1 to 21.
private func contrast(_ first: PaletteColor, _ second: PaletteColor) -> Double {
  func linear(_ value: Double) -> Double {
    value <= 0.04045 ? value / 12.92 : pow((value + 0.055) / 1.055, 2.4)
  }
  func luminance(_ color: PaletteColor) -> Double {
    0.2126 * linear(color.red) + 0.7152 * linear(color.green) + 0.0722 * linear(color.blue)
  }
  let lighter = max(luminance(first), luminance(second))
  let darker = min(luminance(first), luminance(second))
  return (lighter + 0.05) / (darker + 0.05)
}

private let normalText = 4.5
private let controlBoundary = 3.0

@Suite("Palette")
struct PaletteTests {
  @Test func theContrastFormulaGivesTheKnownValues() {
    let black = PaletteColor(hex: 0x000000)
    let white = PaletteColor(hex: 0xFFFFFF)
    #expect(abs(contrast(black, white) - 21) < 0.001)
    #expect(contrast(white, white) == 1)
    #expect(abs(contrast(PaletteColor(hex: 0x777777), white) - 4.48) < 0.01)
  }

  @Test func aMixIsTheSameAsInTheStylesheetOfTheWebsite() {
    #expect(Palette.textSecondary == Palette.cream.mixed(0.80, with: Palette.dark))
    let half = PaletteColor(hex: 0x000000).mixed(0.5, with: PaletteColor(hex: 0xFFFFFF))
    #expect(half == PaletteColor(red: 0.5, green: 0.5, blue: 0.5))
    #expect(Palette.cream.mixed(1, with: Palette.dark) == Palette.cream)
    #expect(Palette.cream.mixed(0, with: Palette.dark) == Palette.dark)
  }

  @Test func aHexValueGivesItsThreeParts() {
    let honey = PaletteColor(hex: 0xF0B53F)
    #expect(honey == PaletteColor(red: 240.0 / 255, green: 181.0 / 255, blue: 63.0 / 255))
  }

  @Test(arguments: [Palette.background, Palette.layer])
  func textIsReadableOnTheGroundAndOnARow(ground: PaletteColor) {
    for text in [Palette.text, Palette.textSecondary, Palette.textHelper, Palette.interactive] {
      #expect(contrast(text, ground) >= normalText)
    }
  }

  @Test func textIsReadableOnARaisedBox() {
    for text in [Palette.text, Palette.textSecondary, Palette.interactive] {
      #expect(contrast(text, Palette.layerRaised) >= normalText)
    }
  }

  @Test(arguments: Tone.allCases)
  func everyToneIsReadableOnTheGroundAndOnARow(tone: Tone) {
    #expect(contrast(tone.color, Palette.background) >= normalText)
    #expect(contrast(tone.color, Palette.layer) >= normalText)
  }

  @Test(arguments: [
    RiskTier.t0, .t1, .t2, .t3, .none, .unknown("T9"),
  ])
  func everyTierTagIsReadable(tier: RiskTier) {
    #expect(contrast(Palette.tagText, tier.tagBackground) >= normalText)
  }

  @Test func everyStateHasAToneFromThePalette() {
    let states = WorkState.known.map(\.state) + [.unknown("Paused")]
    for state in states {
      #expect(Tone.allCases.contains(state.tone))
      #expect(contrast(state.tone.color, Palette.layer) >= normalText)
    }
  }

  @Test func textOnHoneyAndInTheChatIsReadable() {
    #expect(contrast(Palette.textOnInteractive, Palette.interactive) >= normalText)
    #expect(contrast(Palette.text, Palette.tagHoney) >= normalText)
    #expect(contrast(Palette.error, Palette.layer) >= normalText)
    #expect(contrast(Palette.success, Palette.layer) >= normalText)
  }

  @Test func theEdgeOfAFieldShowsOnARow() {
    #expect(contrast(Palette.fieldBorder, Palette.layer) >= controlBoundary)
    #expect(contrast(Palette.fieldBorder, Palette.background) >= controlBoundary)
  }

  @Test func theMarkShowsOnItsGround() {
    #expect(contrast(Palette.cream, Palette.markGround) >= controlBoundary)
    #expect(contrast(Palette.honey, Palette.markGround) >= controlBoundary)
  }
}
