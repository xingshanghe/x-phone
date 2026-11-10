# ADR 0003: Device wall streaming and control

## Status

Proposed

## Context

ADR 0001 chose a WebSocket of JPEG frames from `adb exec-out screencap -p` for browser viewing, with taps sent as `adb shell input tap`, and stated that the console does not embed the scrcpy video protocol. That decision was made for a single device detail page.

The console now has to show a device wall (many devices on one screen) and drive several devices from one action. Under ADR 0001 both requirements fail:

- Each frame is a full PNG. A 720x1280 frame is roughly 500 KB before encoding. A wall of tens of devices cannot carry that, and the current path cannot lower the frame rate per viewer because every browser shares one unthrottled loop.
- A tap costs one `subprocess.run(["adb", "shell", "input", "tap", ...])`. Measured cost is 200-500 ms per call. Fanning one action out to 100 devices serially takes more than 30 seconds, which makes multi-control unusable.
- `tools/phone/phone/stream.py` reads control messages only after each capture returns, and its capture timeout is 20 seconds, so a slow `screencap` delays input. Frames and control share one TCP byte stream, so a large frame blocks the small control message behind it.

[NetrisTV/ws-scrcpy](https://github.com/NetrisTV/ws-scrcpy) is MIT licensed and shows that a browser can carry the scrcpy protocol end to end: H.264 over WebSocket, decoded with WebCodecs, plus a control protocol covering multi-touch, wheel scrolling, keyboard, and text injection.

That project is not a fit as a base, because it assumes one host with `adb` directly attached, discovers devices with `adbkit`, has no notion of hosts, instances, groups, or tasks, and has no authorization at any level. Its README warns that the modified server listens on all interfaces, keeps running after the last client disconnects, and that there is no encryption or authorization.

## Decision

1. **Video comes from upstream scrcpy-server.** The jar is pushed to the container and started with `app_process`; the Go agent reaches it over a plain TCP socket through `adb forward`. The agent relays H.264 without decoding it. The browser decodes with the WebCodecs player, ported from ws-scrcpy under MIT.
2. **We do not adopt the ws-scrcpy WebSocket fork of scrcpy-server.** That fork exists so a browser can connect to the device directly. We relay through the Go Hub instead, so the extra WebSocket hop is dead weight and the listening-on-all-interfaces behaviour is a liability. Upstream plain TCP over `adb forward` is one hop less.
3. **This revises ADR 0001 line 20: the console does embed the scrcpy protocol.** scrcpy is Apache-2.0 and ws-scrcpy is MIT. Only the WebCodecs player is ported. The wasm decoders (Broadway, tinyh264, h264-converter) are deliberately not vendored, to keep their licenses and roughly 1 MB of payload out of the bundle.
4. **Control uses the persistent scrcpy control socket, not `adb shell input`.** Injection costs 5-20 ms per event instead of 200-500 ms. Events are expressed with normalized coordinates in the range 0..1; the agent multiplies by the instance `DeviceProfile` width and height, so one action lands correctly on devices with different resolutions.
5. **Frames and control travel on separate channels.** Frames are droppable when a consumer is slow. Control messages are small, ordered, and never dropped. One instance keeps one frame channel and one control channel.
6. **Authorization stays in the Go Hub.** scrcpy-server is never exposed on a routable interface and is never reachable from a browser without going through the Hub.
7. **The JPEG frame path stays as a fallback.** WebCodecs is available in Chromium and derivatives only, so non-Chromium browsers keep the ADR 0001 path.
8. **Multi-control adds a leader and followers.** One selected device is the leader and is shown large; actions taken on it fan out to the followers. Different devices are driven in parallel, the same device is driven through a serial queue so its own event order is preserved.
9. **Subscriptions are explicit.** A tile on the wall subscribes when it scrolls into the viewport and unsubscribes when it leaves. Frame rate is tiered: wall roughly 1 fps at 160 px, preview 5 fps, focused device 10-15 fps. The real rate for one instance is the highest tier among its subscribers.

## Consequences

- The stream loop in `tools/phone/phone/stream.py` is replaced. Capture and control become independent concurrent tasks, which removes the coupling where a slow capture delays input.
- `internal/httpapi/stream.go` changes from one agent connection per instance to one connection per channel per instance, and gains subscribe and unsubscribe control messages.
- The `-simulate` agent stays useful: it can emit synthetic frames on the same channel, so the wall and fan-out UI are testable on macOS without Docker.
- Compatibility between the scrcpy-server jar and Android 14 on Redroid is unverified. A spike must prove it before any of this is built.
- `tools/phone/phone/lifecycle.py` must start scrcpy-server as part of bringing a container up and restart it after a container restart.
- Because Hub broadcasts to every browser attached to an instance, concurrent operators can fight over one device. A control lock with a TTL is required for multi-control, independent of this ADR.
- WebCodecs support is a browser requirement for the high quality path. Operators on Safari or Firefox get the JPEG fallback, not a broken page.
