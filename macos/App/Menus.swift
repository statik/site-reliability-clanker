import AppKit
import WebKit

/// The menu bar. The Edit menu makes copy and paste work in the pages.
enum Menus {
    static func build() -> NSMenu {
        let bar = NSMenu()
        bar.addItem(submenu(appMenu()))
        bar.addItem(submenu(editMenu()))
        bar.addItem(submenu(viewMenu()))
        let window = windowMenu()
        bar.addItem(submenu(window))
        NSApp.windowsMenu = window
        return bar
    }

    private static func submenu(_ menu: NSMenu) -> NSMenuItem {
        let item = NSMenuItem()
        item.submenu = menu
        return item
    }

    private static func item(
        _ title: String, _ action: Selector, _ key: String = "",
        _ modifiers: NSEvent.ModifierFlags = .command
    ) -> NSMenuItem {
        let item = NSMenuItem(title: title, action: action, keyEquivalent: key)
        item.keyEquivalentModifierMask = modifiers
        return item
    }

    private static func appMenu() -> NSMenu {
        let menu = NSMenu()
        let about = #selector(NSApplication.orderFrontStandardAboutPanel(_:))
        menu.addItem(item("About S.R.C.", about))
        menu.addItem(.separator())
        menu.addItem(item("Hide S.R.C.", #selector(NSApplication.hide(_:)), "h"))
        menu.addItem(item(
            "Hide Others", #selector(NSApplication.hideOtherApplications(_:)), "h",
            [.command, .option]
        ))
        menu.addItem(item("Show All", #selector(NSApplication.unhideAllApplications(_:))))
        menu.addItem(.separator())
        menu.addItem(item(
            "Reset Demo Environment…", #selector(AppDelegate.resetDemoEnvironment(_:))
        ))
        menu.addItem(.separator())
        menu.addItem(item("Quit S.R.C.", #selector(NSApplication.terminate(_:)), "q"))
        return menu
    }

    private static func editMenu() -> NSMenu {
        let menu = NSMenu(title: "Edit")
        menu.addItem(item("Undo", Selector(("undo:")), "z"))
        menu.addItem(item("Redo", Selector(("redo:")), "z", [.command, .shift]))
        menu.addItem(.separator())
        menu.addItem(item("Cut", #selector(NSText.cut(_:)), "x"))
        menu.addItem(item("Copy", #selector(NSText.copy(_:)), "c"))
        menu.addItem(item("Paste", #selector(NSText.paste(_:)), "v"))
        menu.addItem(item("Select All", #selector(NSText.selectAll(_:)), "a"))
        return menu
    }

    private static func viewMenu() -> NSMenu {
        let menu = NSMenu(title: "View")
        menu.addItem(item("Reload", #selector(WKWebView.reload(_:)), "r"))
        menu.addItem(item("Back", #selector(WKWebView.goBack(_:)), "["))
        menu.addItem(item("Forward", #selector(WKWebView.goForward(_:)), "]"))
        return menu
    }

    private static func windowMenu() -> NSMenu {
        let menu = NSMenu(title: "Window")
        menu.addItem(item("Minimize", #selector(NSWindow.performMiniaturize(_:)), "m"))
        menu.addItem(item("Close", #selector(NSWindow.performClose(_:)), "w"))
        return menu
    }
}
