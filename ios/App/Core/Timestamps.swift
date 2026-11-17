import Foundation

/// Reads and writes the timestamps of the control plane: UTC with milliseconds.
enum Timestamps {
  private static let style = Date.ISO8601FormatStyle(includingFractionalSeconds: true)
  private static let styleWithoutFraction = Date.ISO8601FormatStyle()
  private static let months = [
    "Jan", "Feb", "Mar", "Apr", "May", "Jun", "Jul", "Aug", "Sep", "Oct", "Nov", "Dec",
  ]

  static let utcCalendar: Calendar = {
    var calendar = Calendar(identifier: .gregorian)
    calendar.timeZone = .gmt
    return calendar
  }()

  static func parse(_ text: String) -> Date? {
    if text.isEmpty { return nil }
    return (try? style.parse(text)) ?? (try? styleWithoutFraction.parse(text))
  }

  /// For example "2026-12-16T03:30:00.000Z", the format the server stores and compares.
  static func format(_ date: Date) -> String {
    date.formatted(style)
  }

  /// For example "16 Dec 2026, 02:27 UTC". The control plane runs on a simulated clock, so
  /// the app shows its times in UTC and says so, and never mixes them with the phone's time.
  static func display(_ text: String) -> String {
    guard let date = parse(text) else { return text }
    return display(date)
  }

  static func display(_ date: Date) -> String {
    let parts = utcCalendar.dateComponents([.year, .month, .day, .hour, .minute], from: date)
    let monthIndex = (parts.month ?? 1) - 1
    let month = months.indices.contains(monthIndex) ? months[monthIndex] : "?"
    let time = String(format: "%02d:%02d", parts.hour ?? 0, parts.minute ?? 0)
    return "\(parts.day ?? 0) \(month) \(parts.year ?? 0), \(time) UTC"
  }

  /// For example "02:07:21", for rows of one timeline.
  static func timeOnly(_ text: String) -> String {
    guard let date = parse(text) else { return text }
    let parts = utcCalendar.dateComponents([.hour, .minute, .second], from: date)
    return String(format: "%02d:%02d:%02d", parts.hour ?? 0, parts.minute ?? 0, parts.second ?? 0)
  }

  /// For example "30 minutes" between two timestamps, or nil when either is unreadable.
  static func span(from start: String, to end: String) -> String? {
    guard let start = parse(start), let end = parse(end), end > start else { return nil }
    let minutes = Int((end.timeIntervalSince(start) / 60).rounded())
    if minutes < 1 { return "less than a minute" }
    return minutes == 1 ? "1 minute" : "\(minutes) minutes"
  }
}

extension String {
  /// The first characters of a commit or plan hash, enough to compare by eye.
  var shortHash: String { String(prefix(10)) }
}
