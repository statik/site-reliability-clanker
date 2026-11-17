import SwiftUI
import UIKit

/// One colour of the palette as sRGB components from 0 to 1. It is a `ShapeStyle`, so a view
/// takes it directly: `.foregroundStyle(Palette.text)`.
struct PaletteColor: ShapeStyle, Equatable, Sendable {
  let red: Double
  let green: Double
  let blue: Double

  init(red: Double, green: Double, blue: Double) {
    self.red = red
    self.green = green
    self.blue = blue
  }

  init(hex: UInt32) {
    self.init(
      red: Double((hex >> 16) & 0xFF) / 255, green: Double((hex >> 8) & 0xFF) / 255,
      blue: Double(hex & 0xFF) / 255)
  }

  /// The same result as `color-mix(in srgb, self share, other)` in the stylesheet of the
  /// website.
  func mixed(_ share: Double, with other: PaletteColor) -> PaletteColor {
    PaletteColor(
      red: red * share + other.red * (1 - share),
      green: green * share + other.green * (1 - share),
      blue: blue * share + other.blue * (1 - share))
  }

  var color: Color { Color(.sRGB, red: red, green: green, blue: blue) }

  var uiColor: UIColor { UIColor(red: red, green: green, blue: blue, alpha: 1) }

  func resolve(in environment: EnvironmentValues) -> Color.Resolved {
    Color.Resolved(red: Float(red), green: Float(green), blue: Float(blue))
  }
}

/// Every colour of the app. The values and the roles are those of the website
/// (`site/css/site.css`): a warm dark ground, cream text and honey for what you can tap.
/// The app has this one theme; it does not follow the light or dark setting of the phone.
enum Palette {
  static let honey = PaletteColor(hex: 0xF0B53F)
  static let cream = PaletteColor(hex: 0xF7ECD7)
  static let sky = PaletteColor(hex: 0x8AA8C8)
  static let seaMid = PaletteColor(hex: 0x3A6E8F)
  static let seaDeep = PaletteColor(hex: 0x1B354A)
  static let dark = PaletteColor(hex: 0x1C1917)
  static let darkWarm = PaletteColor(hex: 0x292524)
  static let terracotta = PaletteColor(hex: 0xE07856)
  static let sage = PaletteColor(hex: 0x8FBC8F)
  static let red = PaletteColor(hex: 0xC65545)

  /// The ground of every screen.
  static let background = dark
  /// The rows of a list and the bars.
  static let layer = darkWarm
  /// A box on a row or on the ground: a tile, a chat bubble, a chip. Only `text` and
  /// `textSecondary` have enough contrast on it.
  static let layerRaised = cream.mixed(0.10, with: darkWarm)
  /// The edge of a field that you type in.
  static let fieldBorder = cream.mixed(0.42, with: dark)
  /// The lines between the rows of a list.
  static let separator = cream.mixed(0.16, with: dark)

  static let text = cream
  static let textSecondary = cream.mixed(0.80, with: dark)
  static let textHelper = cream.mixed(0.64, with: dark)

  /// Buttons, links and the selected item. Text on it is `textOnInteractive`.
  static let interactive = honey
  static let textOnInteractive = dark

  static let success = sage
  static let warning = terracotta
  static let error = red.mixed(0.62, with: cream)
  /// Work that runs now.
  static let active = sky

  /// Text on each of the tag backgrounds.
  static let tagText = cream
  static let tagGray = layerRaised
  static let tagSea = seaMid.mixed(0.60, with: dark)
  static let tagSky = sky.mixed(0.34, with: dark)
  static let tagRed = red.mixed(0.48, with: dark)
  static let tagHoney = honey.mixed(0.30, with: dark)

  /// The ground behind the mark of the product, as in the icon.
  static let markGround = seaDeep
}

/// A list with the ground, the row colour and the text colours of the palette.
struct PaletteList<Content: View>: View {
  @ViewBuilder let content: Content

  var body: some View {
    List {
      Group { content }
        .listRowBackground(Palette.layer.color)
        .listRowSeparatorTint(Palette.separator.color)
    }
    .paletteGround()
  }
}

/// A form with the ground, the row colour and the text colours of the palette.
struct PaletteForm<Content: View>: View {
  @ViewBuilder let content: Content

  var body: some View {
    Form {
      Group { content }
        .listRowBackground(Palette.layer.color)
        .listRowSeparatorTint(Palette.separator.color)
    }
    .paletteGround()
  }
}

extension View {
  /// Gives a list, a form or a scroll view the ground and the text colours of the palette.
  func paletteGround() -> some View {
    scrollContentBackground(.hidden)
      .background(Palette.background)
      .foregroundStyle(Palette.text, Palette.textSecondary, Palette.textHelper)
  }

  /// Gives a window or a sheet the accent of the palette.
  func paletteRoot() -> some View {
    tint(Palette.interactive.color)
  }
}

extension Palette {
  /// Colours the titles of the navigation bars and the items of the tab bar. SwiftUI has no
  /// modifier for these, so UIKit gets the colours before the first view exists.
  @MainActor static func applyToBars() {
    let title: [NSAttributedString.Key: Any] = [.foregroundColor: text.uiColor]
    UINavigationBar.appearance().titleTextAttributes = title
    UINavigationBar.appearance().largeTitleTextAttributes = title
  }
}

/// A button in a box, for a button that is not a row of a list.
struct PaletteBoxButtonStyle: ButtonStyle {
  @Environment(\.isEnabled) private var isEnabled

  func makeBody(configuration: Configuration) -> some View {
    configuration.label
      .foregroundStyle(Palette.interactive)
      .padding(.horizontal, 12)
      .padding(.vertical, 7)
      .background(Palette.layerRaised, in: RoundedRectangle(cornerRadius: 10))
      .opacity(configuration.isPressed || !isEnabled ? 0.5 : 1)
  }
}
