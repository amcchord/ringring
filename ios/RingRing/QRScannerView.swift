import AVFoundation
import SwiftUI

struct QRScannerView: UIViewControllerRepresentable {
    let onCode: (String) -> Void
    let onError: (String) -> Void

    func makeCoordinator() -> Coordinator {
        Coordinator(onCode: onCode, onError: onError)
    }

    func makeUIViewController(context: Context) -> ScannerViewController {
        let controller = ScannerViewController()
        controller.onCode = context.coordinator.receive
        controller.onError = context.coordinator.fail
        return controller
    }

    func updateUIViewController(_ uiViewController: ScannerViewController, context: Context) {}

    final class Coordinator {
        private let onCode: (String) -> Void
        private let onError: (String) -> Void
        private var delivered = false

        init(onCode: @escaping (String) -> Void, onError: @escaping (String) -> Void) {
            self.onCode = onCode
            self.onError = onError
        }

        func receive(_ value: String) {
            guard !delivered else { return }
            delivered = true
            onCode(value)
        }

        func fail(_ message: String) {
            onError(message)
        }
    }
}

final class ScannerViewController: UIViewController, AVCaptureMetadataOutputObjectsDelegate {
    var onCode: ((String) -> Void)?
    var onError: ((String) -> Void)?

    private let session = AVCaptureSession()
    private var previewLayer: AVCaptureVideoPreviewLayer?

    override func viewDidLoad() {
        super.viewDidLoad()
        view.backgroundColor = UIColor(RingRingTheme.ink)
        checkPermissionAndStart()
    }

    override func viewDidLayoutSubviews() {
        super.viewDidLayoutSubviews()
        previewLayer?.frame = view.bounds
    }

    override func viewWillDisappear(_ animated: Bool) {
        super.viewWillDisappear(animated)
        if session.isRunning {
            DispatchQueue.global(qos: .userInitiated).async { [session] in session.stopRunning() }
        }
    }

    private func checkPermissionAndStart() {
        switch AVCaptureDevice.authorizationStatus(for: .video) {
        case .authorized:
            configureAndStart()
        case .notDetermined:
            AVCaptureDevice.requestAccess(for: .video) { [weak self] allowed in
                DispatchQueue.main.async {
                    allowed ? self?.configureAndStart() : self?.showPermissionError()
                }
            }
        default:
            showPermissionError()
        }
    }

    private func configureAndStart() {
        guard session.inputs.isEmpty else { return }
        guard let camera = AVCaptureDevice.default(for: .video) else {
            onError?("This device doesn’t have a camera available.")
            return
        }
        do {
            let input = try AVCaptureDeviceInput(device: camera)
            let output = AVCaptureMetadataOutput()
            guard session.canAddInput(input), session.canAddOutput(output) else {
                throw ScannerError.unavailable
            }
            session.addInput(input)
            session.addOutput(output)
            output.setMetadataObjectsDelegate(self, queue: .main)
            output.metadataObjectTypes = [.qr]

            let preview = AVCaptureVideoPreviewLayer(session: session)
            preview.videoGravity = .resizeAspectFill
            view.layer.insertSublayer(preview, at: 0)
            previewLayer = preview
            preview.frame = view.bounds

            DispatchQueue.global(qos: .userInitiated).async { [session] in session.startRunning() }
        } catch {
            onError?("RingRing couldn’t start the camera.")
        }
    }

    private func showPermissionError() {
        onError?("Camera access is off. Turn it on in Settings, or paste the setup link instead.")
    }

    func metadataOutput(
        _ output: AVCaptureMetadataOutput,
        didOutput metadataObjects: [AVMetadataObject],
        from connection: AVCaptureConnection
    ) {
        guard let object = metadataObjects.first as? AVMetadataMachineReadableCodeObject,
              let value = object.stringValue else { return }
        onCode?(value)
    }
}

private enum ScannerError: Error {
    case unavailable
}
