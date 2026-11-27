import type { HdmiStates, USBStates } from "@hooks/stores";
import { m } from "@localizations/messages.js";

export type Browser = "chrome" | "edge" | "firefox" | "safari" | "other";

export interface Facts {
  mode: "local" | "cloud";
  browser: Browser;
  linux: boolean;
  addressIsIp: boolean;
  webrtcSupported: boolean;
  h264Supported: boolean;
  computerOnline: boolean;

  socketOpen: boolean;
  httpReachable: boolean | null;
  cloudOnline: boolean | null;
  answerState: "none" | "accepted" | "rejected";
  reachedThroughOtherAddress: boolean | null;
  selfTestConnected: boolean | null;
  selfTestH264: boolean | null;
  iceWasConnected: boolean;
  peerConnected: boolean;

  hdmiState: HdmiStates;
  usbState: USBStates;
  edidIsDefault: boolean | null;

  receivingBytes: boolean;
  decodingFrames: boolean;
  codec: "H.264" | "H.265" | null;
  videoPlaying: boolean;
  playError: string | null;

  msWaiting: number;
}

export type Level = "certain" | "try_first" | "issue";

export type CardAction =
  | "wake"
  | "play"
  | "restoreEdid"
  | "reconnect"
  | "update"
  | "restart"
  | "videoSettings"
  | "edidSweep";

export interface Link {
  label: string;
  href: string;
}

export interface Step {
  title: string;
  detail?: string;
  alternative?: boolean;
  action?: CardAction;
  link?: Link;
}

export interface Finding {
  question: number;
  title: string;
  summary: string;
  level: Level;
  steps: Step[];
  canReconnect: boolean;
}

export type Diagnosis =
  | { status: "ok" }
  | { status: "waiting"; question: number; title: string }
  | ({ status: "failed" } & Finding);

export const WAIT_LIMIT_MS = 10_000;

export const questionNames = () => [
  m.diagnosis_check_webrtc(),
  m.diagnosis_check_h264(),
  m.diagnosis_check_kvm_answers(),
  m.diagnosis_check_network_path(),
  m.diagnosis_check_secure_connection(),
  m.diagnosis_check_hdmi_signal(),
  m.diagnosis_check_video_from_kvm(),
  m.diagnosis_check_video_decoding(),
  m.diagnosis_check_playback(),
];

const waitingTitles: Record<number, () => string> = {
  3: m.diagnosis_waiting_connecting,
  4: m.diagnosis_waiting_network_path,
  5: m.diagnosis_waiting_completing,
  6: m.diagnosis_waiting_hdmi,
  7: m.diagnosis_waiting_video,
  8: m.diagnosis_waiting_decoding,
  9: m.diagnosis_waiting_playback,
};

const failed = (
  question: number,
  title: string,
  summary: string,
  level: Level,
  steps: Step[] = [],
): Diagnosis => ({
  status: "failed",
  question,
  title,
  summary,
  level,
  steps,
  canReconnect: question === 4 || question === 5,
});

const privateWindowTitles: Partial<Record<Browser, () => string>> = {
  chrome: m.diagnosis_step_private_window_chrome,
  edge: m.diagnosis_step_private_window_edge,
};

const privateWindow = (facts: Facts): Step => ({
  title: (privateWindowTitles[facts.browser] ?? m.diagnosis_step_private_window)(),
  detail: m.diagnosis_step_private_window_detail(),
});

const flathubFirefox = (): Step => ({
  title: m.diagnosis_step_flathub_firefox(),
  alternative: true,
});

const restoreEdid = (): Step => ({
  title: m.diagnosis_step_restore_edid(),
  detail: m.diagnosis_step_restore_edid_detail(),
  action: "restoreEdid",
});

const withEdid = (facts: Facts, steps: Step[]) =>
  facts.edidIsDefault === false ? [restoreEdid(), ...steps] : steps;

const otherBrowser = (): Step => ({ title: m.diagnosis_step_other_browser() });

function noWebrtc(facts: Facts): Diagnosis {
  const steps: Step[] = [];
  if (facts.browser === "firefox") {
    steps.push({
      title: m.diagnosis_step_enable_webrtc(),
      detail: m.diagnosis_step_enable_webrtc_detail(),
    });
  }
  steps.push(privateWindow(facts));
  return failed(
    1,
    m.diagnosis_no_webrtc_title(),
    m.diagnosis_no_webrtc_summary(),
    "certain",
    steps,
  );
}

function firefoxH264Steps(facts: Facts): Step[] {
  const steps: Step[] = [
    {
      title: m.diagnosis_step_firefox_hw_h264(),
      detail: m.diagnosis_step_firefox_hw_h264_detail(),
    },
    {
      title: m.diagnosis_step_openh264(),
      detail: m.diagnosis_step_openh264_detail(),
    },
    { title: m.diagnosis_step_restart_firefox() },
  ];
  if (facts.linux) steps.push(flathubFirefox());
  return steps;
}

function noH264(facts: Facts): Diagnosis {
  const steps: Step[] = facts.browser === "firefox" ? firefoxH264Steps(facts) : [otherBrowser()];
  return failed(2, m.diagnosis_no_h264_title(), m.diagnosis_no_h264_summary(), "certain", steps);
}

function socketClosed(facts: Facts): Diagnosis {
  if (!facts.computerOnline) {
    return failed(3, m.diagnosis_offline_title(), m.diagnosis_offline_summary(), "certain", [
      { title: m.diagnosis_step_check_computer_network() },
    ]);
  }

  if (facts.mode === "cloud") {
    if (facts.cloudOnline === false) {
      return failed(
        3,
        m.diagnosis_cloud_offline_title(),
        m.diagnosis_cloud_offline_summary(),
        "certain",
        [
          { title: m.diagnosis_step_check_kvm_power() },
          {
            title: m.diagnosis_step_open_locally(),
            detail: m.diagnosis_step_open_locally_detail(),
          },
        ],
      );
    }
    return failed(
      3,
      m.diagnosis_no_response_title(),
      m.diagnosis_cloud_no_response_summary(),
      "issue",
    );
  }

  if (facts.httpReachable !== true) {
    return failed(
      3,
      m.diagnosis_no_response_title(),
      m.diagnosis_local_no_response_summary(),
      "certain",
      [
        { title: m.diagnosis_step_check_power_cable() },
        { title: m.diagnosis_step_check_ip(), detail: m.diagnosis_display_shows_address() },
        {
          title: m.diagnosis_step_same_network(),
          detail: m.diagnosis_step_same_network_detail(),
        },
      ],
    );
  }

  const title = m.diagnosis_socket_blocked_title();
  const summary = m.diagnosis_socket_blocked_summary();
  if (facts.browser === "safari" && facts.addressIsIp) {
    return failed(3, title, summary, "certain", [
      {
        title: m.diagnosis_step_use_hostname(),
        detail: m.diagnosis_step_use_hostname_detail(),
        link: {
          label: m.diagnosis_link_workarounds(),
          href: "https://github.com/jetkvm/kvm/issues/1098#issuecomment-3878012741",
        },
      },
      { ...otherBrowser(), alternative: true },
    ]);
  }
  return failed(3, title, summary, "issue");
}

function browserWebrtcSteps(facts: Facts): Step[] {
  const steps: Step[] = [privateWindow(facts)];
  if (facts.browser === "chrome") {
    steps.push(
      {
        title: m.diagnosis_step_reset_ip_policy(),
        detail: m.diagnosis_step_reset_ip_policy_detail(),
      },
      {
        title: m.diagnosis_step_vivaldi_broadcast_ip(),
        detail: m.diagnosis_step_vivaldi_broadcast_ip_detail(),
      },
    );
  }
  return steps;
}

function networkBlocked(facts: Facts): Diagnosis {
  if (facts.answerState === "rejected") {
    return failed(
      4,
      m.diagnosis_answer_rejected_title(),
      m.diagnosis_answer_rejected_summary(),
      "issue",
    );
  }
  if (facts.answerState === "none") {
    return failed(4, m.diagnosis_no_answer_title(), m.diagnosis_no_answer_summary(), "issue");
  }
  if (facts.selfTestConnected === false) {
    return failed(
      4,
      m.diagnosis_self_test_failed_title(),
      m.diagnosis_self_test_failed_summary(),
      "try_first",
      browserWebrtcSteps(facts),
    );
  }
  if (facts.reachedThroughOtherAddress) {
    return failed(
      4,
      m.diagnosis_other_address_title(),
      m.diagnosis_other_address_summary(),
      "try_first",
      [
        { title: m.diagnosis_step_own_address(), detail: m.diagnosis_display_shows_address() },
        { title: m.diagnosis_step_remote_access(), alternative: true },
      ],
    );
  }

  const networkSteps: Step[] = [{ title: m.diagnosis_step_disconnect_vpn() }];
  if (facts.mode === "cloud") networkSteps.push({ title: m.diagnosis_step_other_network() });
  if (facts.selfTestConnected) {
    return failed(
      4,
      m.diagnosis_network_blocked_title(),
      m.diagnosis_network_blocked_summary_self_test(),
      "try_first",
      networkSteps,
    );
  }
  return failed(
    4,
    m.diagnosis_network_blocked_title(),
    m.diagnosis_network_blocked_summary(),
    "try_first",
    [networkSteps[0], ...browserWebrtcSteps(facts), ...networkSteps.slice(1)],
  );
}

function notCompleted(): Diagnosis {
  return failed(5, m.diagnosis_not_completed_title(), m.diagnosis_not_completed_summary(), "issue");
}

const usbObservations: Partial<Record<USBStates, () => string>> = {
  suspended: m.diagnosis_usb_suspended,
  "not attached": m.diagnosis_usb_not_attached,
  configured: m.diagnosis_usb_configured,
};

const wakeComputer = (): Step => ({ title: m.diagnosis_step_wake(), action: "wake" });
const checkPower = (): Step => ({ title: m.diagnosis_step_check_computer_on() });
const checkHdmiCable = (): Step => ({ title: m.diagnosis_step_check_hdmi_cable() });
const lowerResolution = (): Step => ({
  title: m.diagnosis_step_lower_resolution(),
  detail: m.diagnosis_step_lower_resolution_detail(),
});

const edidSweep = (): Step => ({
  title: m.diagnosis_step_edid_sweep(),
  detail: m.diagnosis_step_edid_sweep_detail(),
  action: "edidSweep",
});

function noSignalSteps(usbState: USBStates): Step[] {
  if (usbState === "suspended") return [wakeComputer(), checkPower(), checkHdmiCable()];
  if (usbState === "not attached") return [checkPower(), checkHdmiCable()];
  return [checkHdmiCable(), wakeComputer(), edidSweep()];
}

function noSignalSummary(usbState: USBStates) {
  const observation = usbObservations[usbState];
  const summary = m.diagnosis_no_signal_summary();
  return observation ? `${summary} ${observation()}` : summary;
}

function hdmiNotReady(facts: Facts): Diagnosis {
  switch (facts.hdmiState) {
    case "no_signal":
      return failed(
        6,
        m.diagnosis_no_signal_title(),
        noSignalSummary(facts.usbState),
        "try_first",
        withEdid(facts, noSignalSteps(facts.usbState)),
      );
    case "out_of_range":
      return failed(
        6,
        m.diagnosis_out_of_range_title(),
        m.diagnosis_out_of_range_summary(),
        "certain",
        withEdid(facts, [lowerResolution(), edidSweep()]),
      );
    case "no_lock":
      return failed(
        6,
        m.diagnosis_no_lock_title(),
        m.diagnosis_no_lock_summary(),
        "try_first",
        withEdid(facts, [checkHdmiCable(), lowerResolution(), edidSweep()]),
      );
    default:
      return failed(
        6,
        m.diagnosis_no_hdmi_state_title(),
        m.diagnosis_no_hdmi_state_summary(),
        "issue",
      );
  }
}

function noVideoData(facts: Facts): Diagnosis {
  return failed(
    7,
    m.diagnosis_no_video_title(),
    m.diagnosis_no_video_summary(),
    "try_first",
    withEdid(facts, [
      { title: m.diagnosis_step_update_firmware(), action: "update" },
      { title: m.diagnosis_step_restart_kvm(), action: "restart" },
    ]),
  );
}

function cannotDecode(facts: Facts): Diagnosis {
  const summary = m.diagnosis_cannot_decode_summary();
  if (facts.codec === "H.265") {
    return failed(8, m.diagnosis_cannot_decode_h265_title(), summary, "certain", [
      { title: m.diagnosis_step_switch_h264(), action: "videoSettings" },
    ]);
  }
  const browserSteps: Step[] =
    facts.browser === "firefox" ? firefoxH264Steps(facts) : [otherBrowser()];
  if (facts.selfTestH264 === false) {
    return failed(
      8,
      m.diagnosis_h264_self_test_failed_title(),
      m.diagnosis_h264_self_test_failed_summary(),
      "try_first",
      browserSteps,
    );
  }
  if (facts.selfTestH264) {
    return failed(
      8,
      m.diagnosis_kvm_video_undecodable_title(),
      m.diagnosis_kvm_video_undecodable_summary(),
      "issue",
    );
  }
  const title = m.diagnosis_cannot_decode_title();
  if (facts.browser === "firefox") {
    return failed(8, title, summary, "try_first", browserSteps);
  }
  return failed(8, title, summary, "issue");
}

function notPlaying(facts: Facts): Diagnosis {
  if (facts.playError === "NotAllowedError") {
    return failed(
      9,
      m.diagnosis_autoplay_blocked_title(),
      m.diagnosis_autoplay_blocked_summary(),
      "certain",
      [
        facts.browser === "edge"
          ? {
              title: m.diagnosis_step_allow_autoplay_edge(),
              detail: m.diagnosis_step_allow_autoplay_edge_detail(),
            }
          : {
              title: m.diagnosis_step_allow_autoplay(),
              detail: m.diagnosis_step_allow_autoplay_detail(),
            },
        { title: m.diagnosis_step_start_video(), action: "play" },
      ],
    );
  }
  return failed(9, m.diagnosis_not_playing_title(), m.diagnosis_not_playing_summary(), "issue");
}

export function diagnose(facts: Facts): Diagnosis {
  if (!facts.webrtcSupported) return noWebrtc(facts);
  if (!facts.h264Supported) return noH264(facts);

  const questions: [number, boolean, (facts: Facts) => Diagnosis][] = [
    [3, facts.socketOpen, socketClosed],
    [4, facts.iceWasConnected, networkBlocked],
    [5, facts.peerConnected, notCompleted],
    [6, facts.hdmiState === "ready", hdmiNotReady],
    [7, facts.receivingBytes, noVideoData],
    [8, facts.decodingFrames, cannotDecode],
    [9, facts.videoPlaying, notPlaying],
  ];

  for (const [question, passed, explain] of questions) {
    if (passed) continue;
    const knownNow =
      (question === 3 && !facts.computerOnline) ||
      (question === 4 && facts.answerState === "rejected");
    if (!knownNow && facts.msWaiting < WAIT_LIMIT_MS) {
      return { status: "waiting", question, title: waitingTitles[question]() };
    }
    return explain(facts);
  }

  return { status: "ok" };
}
