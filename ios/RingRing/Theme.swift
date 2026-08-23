import SwiftUI

enum RingRingTheme {
    static let ink = Color(red: 0.11, green: 0.10, blue: 0.23)
    static let purple = Color(red: 0.42, green: 0.30, blue: 1.00)
    static let coral = Color(red: 1.00, green: 0.42, blue: 0.48)
    static let yellow = Color(red: 1.00, green: 0.85, blue: 0.30)
    static let blue = Color(red: 0.22, green: 0.78, blue: 0.96)
    static let mint = Color(red: 0.45, green: 0.88, blue: 0.69)
    static let canvas = Color(red: 1.00, green: 0.97, blue: 0.94)

    static let cardShadow = ink.opacity(0.18)
}

struct MemphisBackground: View {
    var body: some View {
        GeometryReader { geometry in
            ZStack {
                RingRingTheme.canvas

                Circle()
                    .fill(RingRingTheme.yellow)
                    .frame(width: 180, height: 180)
                    .offset(x: geometry.size.width * 0.40, y: -geometry.size.height * 0.42)

                RoundedRectangle(cornerRadius: 32, style: .continuous)
                    .fill(RingRingTheme.blue.opacity(0.55))
                    .frame(width: 128, height: 128)
                    .rotationEffect(.degrees(18))
                    .offset(x: -geometry.size.width * 0.46, y: geometry.size.height * 0.36)

                MemphisSquiggle()
                    .stroke(RingRingTheme.purple.opacity(0.28), style: StrokeStyle(lineWidth: 8, lineCap: .round, lineJoin: .round))
                    .frame(width: 110, height: 54)
                    .rotationEffect(.degrees(-14))
                    .offset(x: geometry.size.width * 0.34, y: geometry.size.height * 0.38)

                Circle()
                    .fill(RingRingTheme.coral.opacity(0.35))
                    .frame(width: 26, height: 26)
                    .offset(x: -geometry.size.width * 0.36, y: -geometry.size.height * 0.31)
            }
            .ignoresSafeArea()
        }
        .accessibilityHidden(true)
    }
}

private struct MemphisSquiggle: Shape {
    func path(in rect: CGRect) -> Path {
        var path = Path()
        path.move(to: CGPoint(x: rect.minX, y: rect.midY))
        path.addLine(to: CGPoint(x: rect.minX + rect.width * 0.22, y: rect.minY))
        path.addLine(to: CGPoint(x: rect.minX + rect.width * 0.48, y: rect.maxY))
        path.addLine(to: CGPoint(x: rect.minX + rect.width * 0.74, y: rect.minY))
        path.addLine(to: CGPoint(x: rect.maxX, y: rect.midY))
        return path
    }
}

struct RingRingCard<Content: View>: View {
    let content: Content

    init(@ViewBuilder content: () -> Content) {
        self.content = content()
    }

    var body: some View {
        ZStack {
            RoundedRectangle(cornerRadius: 30, style: .continuous)
                .fill(.white.opacity(0.94))
                .shadow(color: RingRingTheme.cardShadow, radius: 0, x: 7, y: 8)
            RoundedRectangle(cornerRadius: 30, style: .continuous)
                .stroke(RingRingTheme.ink.opacity(0.12), lineWidth: 1)
            content.padding(22)
        }
    }
}

struct PrimaryButtonStyle: ButtonStyle {
    var color = RingRingTheme.purple

    func makeBody(configuration: Configuration) -> some View {
        configuration.label
            .font(.headline.weight(.bold))
            .foregroundStyle(.white)
            .frame(maxWidth: .infinity, minHeight: 56)
            .background(configuration.isPressed ? color.opacity(0.78) : color, in: RoundedRectangle(cornerRadius: 18, style: .continuous))
            .scaleEffect(configuration.isPressed ? 0.98 : 1)
            .animation(.easeOut(duration: 0.12), value: configuration.isPressed)
    }
}
