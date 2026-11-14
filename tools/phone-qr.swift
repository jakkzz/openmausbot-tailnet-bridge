import Foundation
import Vision
import AppKit
import CoreImage
import Darwin

// Protect new output from its first byte, including direct codec invocations.
umask(0o077)

func fail(_ message: String) -> Never { fputs(message + "\n", stderr); exit(1) }
func writePrivate(_ data: Data, to path: String) throws {
    let fd = open(path, O_WRONLY | O_CREAT | O_EXCL, mode_t(0o600))
    guard fd >= 0 else { fail("Cannot create private output; destination may exist") }
    let handle = FileHandle(fileDescriptor: fd, closeOnDealloc: true)
    defer { try? handle.close() }
    try handle.write(contentsOf: data)
}
let args = CommandLine.arguments
if args.count != 4 && args.count != 8 { fail("Usage: phone-qr decode IMAGE PRIVATE_TEXT [X Y W H] | encode PRIVATE_TEXT PNG") }
guard !FileManager.default.fileExists(atPath: args[3]) else { fail("Output exists; refusing overwrite") }
if args[1] == "decode" {
    do {
        guard var source = CIImage(contentsOf: URL(fileURLWithPath: args[2])) else { fail("Image unavailable") }
        if args.count == 8 {
            guard let x = Double(args[4]), let y = Double(args[5]), let w = Double(args[6]), let h = Double(args[7]), x.isFinite, y.isFinite, w.isFinite, h.isFinite, w > 0, h > 0 else { fail("Invalid crop") }
            source = source.cropped(to: CGRect(x: x, y: source.extent.height-y-h, width: w, height: h))
            source = source.transformed(by: CGAffineTransform(translationX: -source.extent.minX, y: -source.extent.minY))
        }
        var found = Set<String>()
        for scale in [1.0, 2.0, 4.0, 8.0] {
            let input = source.samplingNearest().transformed(by: CGAffineTransform(scaleX: scale, y: scale))
            let request = VNDetectBarcodesRequest()
            request.symbologies = [.qr]
            try VNImageRequestHandler(ciImage: input).perform([request])
            for item in request.results ?? [] {
                if let text = item.payloadStringValue, text.hasPrefix("openmausbot://pair?") { found.insert(text) }
            }
            if !found.isEmpty { break }
        }
        let matches = Array(found)
        guard matches.count == 1 else { fail("Expected exactly one OpenMausBot QR; found \(matches.count)") }
        try writePrivate(Data(matches[0].utf8), to: args[3])
        print("Private OpenMausBot QR decoded to owner-only file")
    } catch { fail("QR decoding failed") }
} else if args[1] == "encode" {
    do {
        let value = try String(contentsOfFile: args[2], encoding: .utf8).trimmingCharacters(in: .whitespacesAndNewlines)
        guard value.hasPrefix("openmausbot://pair?"), let data = value.data(using: .utf8), let filter = CIFilter(name: "CIQRCodeGenerator") else { fail("Invalid pairing link") }
        filter.setValue(data, forKey: "inputMessage")
        filter.setValue("M", forKey: "inputCorrectionLevel")
        guard let output = filter.outputImage?.transformed(by: CGAffineTransform(scaleX: 8, y: 8)) else { fail("QR generation failed") }
        let context = CIContext()
        guard let cg = context.createCGImage(output, from: output.extent) else { fail("QR rendering failed") }
        let image = NSBitmapImageRep(cgImage: cg)
        guard let png = image.representation(using: .png, properties: [:]) else { fail("PNG rendering failed") }
        try writePrivate(png, to: args[3])
        print("Tailscale-only pairing QR saved privately")
    } catch { fail("QR encoding failed") }
} else { fail("Unknown operation") }
