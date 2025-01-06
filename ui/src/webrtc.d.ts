interface RTCIceCandidateStats {
  address?: string; // The address of the candidate. Could be IPv4, IPv6, or a fully-qualified domain name.
  candidateType: "host" | "srflx" | "prflx" | "relay"; // The type of candidate (host, srflx, prflx, relay).
  foundation: string; // A unique identifier for this candidate, used for network performance optimization.
  id: string; // A unique identifier for this object.
  port?: number; // The network port used by the candidate.
  priority?: number; // The priority of the candidate.
  protocol?: string; // The protocol used by the candidate (tcp or udp).
  relatedAddress?: string; // The related address of the candidate.
  relatedPort?: number; // The related port of the candidate.
  sdpMid?: string; // The media stream identification for the candidate.
  sdpMLineIndex?: number; // The index of the media line for the candidate.
  tcpType?: string; // The type of TCP candidate (active, passive, or so).
  type: "local-candidate" | "remote-candidate"; // The type of the statistics object.
  usernameFragment: string; // The username fragment used for message authentication.
  timestamp: number; // The timestamp at which the sample was taken.
}

interface RTCDataChannelStats {
  bytesReceived: number;
  bytesSent: number;
  dataChannelIdentifier: number;
  id: string;
  label: string;
  messagesReceived: number;
  messagesSent: number;
  protocol: string;
  state: string;
  timestamp: number;
  type: string;
}
