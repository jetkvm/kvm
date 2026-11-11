// Prints what a macOS keyboard layout types for each key and modifier
// combination, as JSON for generate_macos_layouts.mjs. For dead keys it also
// prints what each following key composes to.
//
// usage: swift dump_keylayout.swift <input source id> <keyboard type> <keys>
//   <input source id>  e.g. com.apple.keylayout.German
//   <keyboard type>    UCKeyTranslate keyboard type, e.g. 43 for ISO
//   <keys>             comma-separated "<HID key name>:<macOS virtual key code>"
import Carbon
import Foundation

let args = CommandLine.arguments
guard args.count == 4, let kbType = UInt32(args[2]) else {
    FileHandle.standardError.write("usage: dump_keylayout.swift <input source id> <keyboard type> <keys>\n".data(using: .utf8)!)
    exit(2)
}
let keys: [(String, UInt16)] = args[3].split(separator: ",").map {
    let parts = $0.split(separator: ":")
    return (String(parts[0]), UInt16(parts[1])!)
}

let filter = [kTISPropertyInputSourceID as String: args[1]] as CFDictionary
guard let sources = TISCreateInputSourceList(filter, true)?.takeRetainedValue() as? [TISInputSource],
      let source = sources.first,
      let layoutPtr = TISGetInputSourceProperty(source, kTISPropertyUnicodeKeyLayoutData) else {
    FileHandle.standardError.write("keyboard layout \(args[1]) not found\n".data(using: .utf8)!)
    exit(1)
}
let layoutData = Unmanaged<CFData>.fromOpaque(layoutPtr).takeUnretainedValue() as Data

// UCKeyTranslate modifier bits: shift (2) and option (8), which a USB
// keyboard sends as Shift and right Alt.
let modifiers: [(String, UInt32)] = [("", 0), ("S", 2), ("A", 8), ("SA", 10)]
let spaceKeyCode: UInt16 = 49

var entries: [[String: Any]] = []
layoutData.withUnsafeBytes { raw in
    let layout = raw.bindMemory(to: UCKeyboardLayout.self).baseAddress!
    func press(_ code: UInt16, _ modifier: UInt32, _ deadState: inout UInt32) -> String {
        var length = 0
        var chars = [UniChar](repeating: 0, count: 8)
        UCKeyTranslate(layout, code, UInt16(kUCKeyActionDown), modifier, kbType, 0,
                       &deadState, chars.count, &length, &chars)
        return String(utf16CodeUnits: chars, count: length)
    }
    for (name, code) in keys {
        for (modName, modifier) in modifiers {
            var deadState: UInt32 = 0
            let output = press(code, modifier, &deadState)
            var entry: [String: Any] = ["key": name, "mods": modName, "out": output, "dead": deadState != 0]
            if deadState != 0 {
                var composed: [String: String] = [:]
                for (baseName, baseCode) in keys {
                    for (baseMod, baseModifier) in modifiers where baseMod == "" || baseMod == "S" {
                        var state = deadState
                        composed["\(baseName)|\(baseMod)"] = press(baseCode, baseModifier, &state)
                    }
                }
                var state = deadState
                composed["Space|"] = press(spaceKeyCode, 0, &state)
                entry["composed"] = composed
            }
            entries.append(entry)
        }
    }
}

let json = try JSONSerialization.data(withJSONObject: ["keys": entries], options: [.sortedKeys])
print(String(data: json, encoding: .utf8)!)
