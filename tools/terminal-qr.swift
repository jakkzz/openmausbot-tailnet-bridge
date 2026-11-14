import Foundation
import AppKit
import CoreImage
import Darwin

umask(0o077)
func fail(_ message: String) -> Never { fputs(message + "\n", stderr); exit(1) }
let args = CommandLine.arguments
let measure = args.count == 3 && args[1] == "--measure"
let png = args.count == 4 && args[1] == "--png"
guard args.count == 2 || measure || png else { fail("Usage: terminal-qr FILE | --measure FILE | --png FILE NEW_PNG") }
let path = args[(measure || png) ? 2 : 1]
guard let raw = try? String(contentsOfFile: path, encoding: .utf8) else { fail("Cannot read private invite") }
let value = raw.trimmingCharacters(in: .whitespacesAndNewlines)
guard value.hasPrefix("openmausbot://pair?"), !value.unicodeScalars.contains(where: { CharacterSet.whitespacesAndNewlines.contains($0) || CharacterSet.controlCharacters.contains($0) }), let data = value.data(using: .utf8), let filter = CIFilter(name: "CIQRCodeGenerator") else { fail("Invalid one-line native invite") }
filter.setValue(data, forKey: "inputMessage")
filter.setValue("L", forKey: "inputCorrectionLevel")
guard let image = filter.outputImage else { fail("QR generation failed") }
let context = CIContext(options: [.useSoftwareRenderer: true])
let quiet = 4
if png {
    let destination = args[3]
    guard !FileManager.default.fileExists(atPath: destination) else { fail("Output exists; refusing overwrite") }
    let bounds = image.extent.insetBy(dx: -CGFloat(quiet), dy: -CGFloat(quiet))
    let white = CIImage(color: CIColor.white).cropped(to: bounds)
    let padded = image.composited(over: white).transformed(by: CGAffineTransform(scaleX: 8, y: 8))
    guard let cg = context.createCGImage(padded, from: padded.extent),
          let bytes = NSBitmapImageRep(cgImage: cg).representation(using: .png, properties: [:]) else { fail("PNG rendering failed") }
    do { try bytes.write(to: URL(fileURLWithPath: destination), options: .withoutOverwriting) }
    catch { fail("Cannot create private PNG") }
    exit(0)
}
guard let cg = context.createCGImage(image, from: image.extent) else { fail("QR bitmap unavailable") }
let bitmap = NSBitmapImageRep(cgImage: cg)
let width = bitmap.pixelsWide + quiet * 2
let height = bitmap.pixelsHigh + quiet * 2
if measure {
    print("{\"columns\":\(width * 2),\"rows\":\(height)}")
    exit(0)
}
func black(_ x: Int, _ y: Int) -> Bool {
    let px = x - quiet, py = y - quiet
    guard px >= 0, py >= 0, px < bitmap.pixelsWide, py < bitmap.pixelsHigh,
          let color = bitmap.colorAt(x: px, y: py)?.usingColorSpace(.deviceRGB) else { return false }
    return color.redComponent < 0.5
}
// Full cell BACKGROUNDS, not Unicode glyphs: fonts/line gaps cannot split a module.
// Two spaces by one terminal row approximately matches ordinary mono cell aspect.
for y in 0..<height {
    var row = "", current: Bool? = nil
    for x in 0..<width {
        let dark = black(x, y)
        if current != dark {
            row += dark ? "\u{001b}[48;5;16m" : "\u{001b}[48;5;231m"
            current = dark
        }
        row += "  "
    }
    row += "\u{001b}[0m"
    print(row)
}
