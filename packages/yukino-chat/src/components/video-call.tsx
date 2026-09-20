/**
 * Copyright (c) 2026 hangtiancheng
 *
 * Permission is hereby granted, free of charge, to any person obtaining a copy
 * of this software and associated documentation files (the "Software"), to deal
 * in the Software without restriction, including without limitation the rights
 * to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
 * copies of the Software, and to permit persons to whom the Software is
 * furnished to do so, subject to the following conditions:
 *
 * The above copyright notice and this permission notice shall be included in
 * all copies or substantial portions of the Software.
 *
 * THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
 * IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
 * FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
 * AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
 * LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
 * OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
 * SOFTWARE.
 */

import { customElement, query, state } from "@yukino.js/lit-jsx";
import { nothing } from "@yukino.js/lit-jsx";
import { RtcManager } from "@/utils/rtc";
import { showToast } from "@/utils/toast";
import { TwElement } from "@/styles/base";
import { Button } from "@/components/ui/button";
import { Badge } from "@/components/ui/card";

const OUTGOING_CALL_TIMEOUT_MS = 30_000;

/**
 * Video call overlay. The owning page calls show() to open the overlay
 * and handleSignal() to feed incoming WebRTC signalling frames.
 */
@customElement("x-video-call")
export class XVideoCall extends TwElement {
  @state() private visible = false;
  @state() private inCall = false;
  @state() private incomingCall = false;

  private rtc = new RtcManager();
  private callTimeout: ReturnType<typeof setTimeout> | null = null;

  @query("#vc-local") localVideo!: HTMLVideoElement;
  @query("#vc-remote") remoteVideo!: HTMLVideoElement;

  override connectedCallback() {
    super.connectedCallback();
    this.rtc.onLocalStream = (stream) => {
      if (this.localVideo) this.localVideo.srcObject = stream;
    };
    this.rtc.onRemoteStream = (stream) => {
      if (this.remoteVideo) this.remoteVideo.srcObject = stream;
    };
    this.rtc.onCallEnded = () => {
      this.clearCallTimeout();
      this.inCall = false;
      this.incomingCall = false;
      showToast("Call ended", "info");
    };
  }

  override disconnectedCallback() {
    // Detach callbacks first so unmount cleanup does not toast.
    this.rtc.onLocalStream = null;
    this.rtc.onRemoteStream = null;
    this.rtc.onCallEnded = null;
    this.clearCallTimeout();
    this.rtc.endCall();
    super.disconnectedCallback();
  }

  protected override updated(changed: Map<string, unknown>) {
    // Re-attach streams if the overlay opens while tracks already exist.
    if (changed.has("visible") && this.visible) {
      if (this.rtc.localStream && this.localVideo) {
        this.localVideo.srcObject = this.rtc.localStream;
      }
      if (this.rtc.remoteStream && this.remoteVideo) {
        this.remoteVideo.srcObject = this.rtc.remoteStream;
      }
    }
  }

  private clearCallTimeout() {
    if (this.callTimeout) {
      clearTimeout(this.callTimeout);
      this.callTimeout = null;
    }
  }

  show() {
    this.visible = true;
  }

  handleSignal(avData: Record<string, unknown>) {
    const signalType = avData.type as string | undefined;
    if (signalType === "receive_call" || signalType === "reject_call") {
      this.clearCallTimeout();
    }
    const result = this.rtc.handleSignal(avData);
    if (result === "incoming_call") {
      this.visible = true;
      this.incomingCall = true;
      showToast("Incoming call", "info");
    }
  }

  private async startCall() {
    await this.rtc.startCall();
    this.inCall = true;
    this.clearCallTimeout();
    this.callTimeout = setTimeout(() => {
      this.callTimeout = null;
      this.rtc.sendEndCall();
      this.inCall = false;
      showToast("No answer", "warning");
    }, OUTGOING_CALL_TIMEOUT_MS);
  }

  private async acceptCall() {
    await this.rtc.acceptCall();
    this.inCall = true;
    this.incomingCall = false;
  }

  private rejectCall() {
    this.rtc.rejectCall();
    this.incomingCall = false;
  }

  private hangUp() {
    this.clearCallTimeout();
    this.rtc.sendEndCall();
    this.inCall = false;
  }

  private closeModal() {
    if (this.inCall) {
      showToast("Please hang up first", "warning");
      return;
    }
    this.visible = false;
    this.incomingCall = false;
  }

  override render() {
    if (!this.visible) return nothing;
    return (
      <div className="bg-foreground/40 fixed inset-0 z-50 flex items-center justify-center backdrop-blur-sm">
        <div className="bg-card animate-in fade-in zoom-in-95 w-[700px] max-w-[90vw] rounded-2xl border p-6 shadow-2xl duration-200">
          <h3 className="mb-5 text-center text-lg font-semibold">Video Call</h3>
          <div className="flex flex-col gap-4">
            <div className="flex justify-center gap-4">
              <div className="bg-muted relative h-56 w-72 overflow-hidden rounded-xl">
                <video
                  id="vc-local"
                  autoPlay
                  playsInline
                  muted
                  className="h-full w-full object-cover"
                />
                <Badge className="absolute bottom-2 left-2">You</Badge>
              </div>
              <div className="bg-muted relative h-56 w-72 overflow-hidden rounded-xl">
                <video
                  id="vc-remote"
                  autoPlay
                  playsInline
                  className="h-full w-full object-cover"
                />
                <Badge className="absolute bottom-2 left-2">Remote</Badge>
              </div>
            </div>

            {this.incomingCall && (
              <p className="text-primary-deep text-center text-sm font-medium">
                Incoming call...
              </p>
            )}

            <div className="flex justify-center gap-3">
              {!this.inCall && !this.incomingCall && (
                <Button size="sm" onClick={() => this.startCall()}>
                  Start Call
                </Button>
              )}
              {this.incomingCall && (
                <>
                  <Button size="sm" onClick={() => this.acceptCall()}>
                    Accept
                  </Button>
                  <Button
                    size="sm"
                    variant="destructive"
                    onClick={() => this.rejectCall()}
                  >
                    Reject
                  </Button>
                </>
              )}
              {this.inCall && (
                <Button
                  size="sm"
                  variant="destructive"
                  onClick={() => this.hangUp()}
                >
                  Hang Up
                </Button>
              )}
              <Button
                size="sm"
                variant="ghost"
                className="text-muted-foreground"
                onClick={() => this.closeModal()}
              >
                Close
              </Button>
            </div>
          </div>
        </div>
      </div>
    );
  }
}

declare global {
  interface HTMLElementTagNameMap {
    "x-video-call": XVideoCall;
  }
}
