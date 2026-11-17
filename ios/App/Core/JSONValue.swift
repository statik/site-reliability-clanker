import Foundation

/// A JSON value of any shape. Audit events carry free-form `data`; this keeps it typed.
enum JSONValue: Decodable, Sendable, Equatable {
  case null
  case bool(Bool)
  case number(Double)
  case string(String)
  case array([JSONValue])
  case object([String: JSONValue])

  init(from decoder: Decoder) throws {
    let container = try decoder.singleValueContainer()
    if container.decodeNil() {
      self = .null
    } else if let value = try? container.decode(Bool.self) {
      self = .bool(value)
    } else if let value = try? container.decode(Double.self) {
      self = .number(value)
    } else if let value = try? container.decode(String.self) {
      self = .string(value)
    } else if let value = try? container.decode([JSONValue].self) {
      self = .array(value)
    } else {
      self = .object(try container.decode([String: JSONValue].self))
    }
  }

  var stringValue: String? {
    if case .string(let value) = self { return value }
    return nil
  }

  var intValue: Int? {
    if case .number(let value) = self { return Int(exactly: value.rounded()) }
    return nil
  }

  var stringArray: [String] {
    guard case .array(let values) = self else { return [] }
    return values.compactMap(\.stringValue)
  }
}

extension KeyedDecodingContainer {
  /// The control plane sends `""`, `null` or nothing for an absent value. All three give "".
  func text(_ key: Key) throws -> String {
    try decodeIfPresent(String.self, forKey: key) ?? ""
  }

  func integer(_ key: Key) throws -> Int {
    try decodeIfPresent(Int.self, forKey: key) ?? 0
  }

  func number(_ key: Key) throws -> Double {
    try decodeIfPresent(Double.self, forKey: key) ?? 0
  }

  func flag(_ key: Key) throws -> Bool {
    try decodeIfPresent(Bool.self, forKey: key) ?? false
  }

  /// Go encodes a nil slice as `null`. Both `null` and a missing key give an empty list.
  func list<T: Decodable>(_ key: Key) throws -> [T] {
    try decodeIfPresent([T].self, forKey: key) ?? []
  }

  func map<T: Decodable>(_ key: Key) throws -> [String: T] {
    try decodeIfPresent([String: T].self, forKey: key) ?? [:]
  }
}
