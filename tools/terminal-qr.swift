import Foundation
import AppKit
import CoreImage
import Darwin

func fail(_ message: String) -> Never { fputs(message + "\n", stderr); exit(1) }
let args = CommandLine.arguments
let measure = args.count == 3 && args[1] == "--measure"
guard args.count == 2 || measure else { fail("Usage: terminal-qr [--measure] PRIVATE_LINK_FILE") }
let path = args[measure ? 2 : 1]
guard let raw = try? String(contentsOfFile: path, encoding: .utf8) else { fail("Cannot read private invite") }
let value = raw.trimmingCharacters(in: .whitespacesAndNewlines)
guard value.hasPrefix("openmausbot://pair?"), !value.unicodeScalars.contains(where: { CharacterSet.whitespacesAndNewlines.contains($0) || CharacterSet.controlCharacters.contains($0) }), let data = value.data(using: .utf8), let filter = CIFilter(name: "CIQRCodeGenerator") else { fail("Invalid one-line native invite") }
filter.setValue(data, forKey: "inputMessage")
// Low correction keeps a full invitation compact enough for an 80-column TUI.
// The terminal has no physical damage; retain four extra white quiet modules.
filter.setValue("L", forKey: "inputCorrectionLevel")
guard let image = filter.outputImage else { fail("QR generation failed") }
let context = CIContext(options: [.useSoftwareRenderer: true])
guard let cg = context.createCGImage(image, from: image.extent) else { fail("QR bitmap unavailable") }
let bitmap = NSBitmapImageRep(cgImage: cg)
let quiet = 4
let width = bitmap.pixelsWide + quiet * 2
let height = bitmap.pixelsHigh + quiet * 2
if measure {
    print("{\"columns\":\(width),\"rows\":\((height + 1) / 2)}")
    exit(0)
}
func black(_ x: Int, _ y: Int) -> Bool {
    let px = x - quiet, py = y - quiet
    guard px >= 0, py >= 0, px < bitmap.pixelsWide, py < bitmap.pixelsHigh,
          let color = bitmap.colorAt(x: px, y: py)?.usingColorSpace(.deviceRGB) else { return false }
    return color.redComponent < 0.5
}
// One column per module, two module rows per cell: square modules in a mono TUI.
for y in stride(from: 0, to: height, by: 2) {
    var row = "\u{001b}[30;47m"
    for x in 0..<width {
        switch (black(x, y), black(x, y + 1)) {
        case (true, true): row += "█"
        case (true, false): row += "▀"
        case (false, true): row += "▄"
        case (false, false): row += " "
        }
    }
    row += "\u{001b}[0m"
    print(row)
}
