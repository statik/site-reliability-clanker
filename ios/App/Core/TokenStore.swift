import Foundation
import Security

/// Keeps the token of the sign-in, one for each control plane. `server` is the key that
/// `AppSettings.serverKey` gives.
protocol TokenStore: Sendable {
  func token(for server: String) throws -> String?
  func save(_ token: String, for server: String) throws
  func remove(for server: String) throws
}

/// A Keychain operation failed. The message says which one and gives the status of iOS.
struct KeychainError: Error, Equatable, LocalizedError {
  let operation: String
  let status: OSStatus

  var errorDescription: String? {
    let detail = SecCopyErrorMessageString(status, nil) as String? ?? "no description"
    return "The Keychain could not \(operation) the sign-in token (status \(status): "
      + "\(detail)). Unlock the phone and try again."
  }
}

/// The token store of the app: generic passwords in the Keychain, which only this app on
/// this phone can read, and only while the phone is unlocked.
struct KeychainTokenStore: TokenStore {
  static let service = "dev.clanker.paperboat-manager.sign-in-token"

  private func query(_ server: String) -> [CFString: Any] {
    [
      kSecClass: kSecClassGenericPassword,
      kSecAttrService: Self.service,
      kSecAttrAccount: server,
    ]
  }

  func token(for server: String) throws -> String? {
    var query = query(server)
    query[kSecReturnData] = true
    query[kSecMatchLimit] = kSecMatchLimitOne
    var found: CFTypeRef?
    let status = SecItemCopyMatching(query as CFDictionary, &found)
    if status == errSecItemNotFound { return nil }
    guard status == errSecSuccess, let data = found as? Data else {
      throw KeychainError(operation: "read", status: status)
    }
    return String(data: data, encoding: .utf8)
  }

  func save(_ token: String, for server: String) throws {
    try remove(for: server)
    var item = query(server)
    item[kSecValueData] = Data(token.utf8)
    item[kSecAttrAccessible] = kSecAttrAccessibleWhenUnlockedThisDeviceOnly
    let status = SecItemAdd(item as CFDictionary, nil)
    guard status == errSecSuccess else { throw KeychainError(operation: "save", status: status) }
  }

  func remove(for server: String) throws {
    let status = SecItemDelete(query(server) as CFDictionary)
    guard status == errSecSuccess || status == errSecItemNotFound else {
      throw KeychainError(operation: "remove", status: status)
    }
  }
}
