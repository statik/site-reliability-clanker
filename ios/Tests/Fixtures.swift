import Foundation
import Testing

@testable import SRC

/// Loads the JSON captured from the live control plane and from Paperboat.
enum Fixture {
  private final class Marker {}

  static func data(_ name: String) throws -> Data {
    let bundle = Bundle(for: Marker.self)
    let url = try #require(
      bundle.url(forResource: name, withExtension: "json", subdirectory: "Fixtures"),
      "The fixture \(name).json is not in the test bundle")
    return try Data(contentsOf: url)
  }

  static func decode<T: Decodable>(_ type: T.Type, _ name: String) throws -> T {
    try JSONDecoder().decode(type, from: try data(name))
  }

  static func decode<T: Decodable>(_ type: T.Type, json: String) throws -> T {
    try JSONDecoder().decode(type, from: Data(json.utf8))
  }
}
