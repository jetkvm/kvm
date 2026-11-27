import { create } from "zustand";

import type { Diagnosis, Facts } from "./diagnose";

interface DiagnosisState {
  live: Facts | null;
  facts: Facts | null;
  diagnosis: Diagnosis;
  overrides: Partial<Facts>;
  playError: string | null;
  edidChanges: number;
  sweepTitle: string | null;
  sweepDiagnosis: Diagnosis | null;
  answerState: Facts["answerState"];
  kvmHostAddresses: string[];
  held: boolean;
  publish: (live: Facts, facts: Facts, diagnosis: Diagnosis) => void;
  setOverride: <K extends keyof Facts>(key: K, value: Facts[K] | undefined) => void;
  setOverrides: (overrides: Partial<Facts>) => void;
  setPlayError: (playError: string | null) => void;
  edidChanged: () => void;
  setSweepTitle: (sweepTitle: string | null) => void;
  sessionStarted: () => void;
  setAnswerState: (answerState: Facts["answerState"]) => void;
  addKvmCandidates: (candidates: string[]) => void;
  setHeld: (held: boolean) => void;
}

const HOST_CANDIDATE = /^(?:a=)?candidate:\S+ \d+ \S+ \d+ (\S+) \d+ typ host/;

export const useDiagnosisStore = create<DiagnosisState>(set => ({
  live: null,
  facts: null,
  diagnosis: { status: "ok" },
  overrides: {},
  playError: null,
  edidChanges: 0,
  sweepTitle: null,
  sweepDiagnosis: null,
  answerState: "none",
  kvmHostAddresses: [],
  held: false,
  publish: (live, facts, diagnosis) => set({ live, facts, diagnosis }),
  setOverride: (key, value) =>
    set(state => {
      const overrides = { ...state.overrides };
      if (value === undefined) delete overrides[key];
      else overrides[key] = value;
      return { overrides };
    }),
  setOverrides: overrides => set({ overrides }),
  setPlayError: playError => set({ playError }),
  edidChanged: () => set(state => ({ edidChanges: state.edidChanges + 1 })),
  sessionStarted: () => set({ answerState: "none", kvmHostAddresses: [] }),
  setAnswerState: answerState => set({ answerState }),
  addKvmCandidates: candidates =>
    set(state => {
      const found = candidates
        .map(candidate => HOST_CANDIDATE.exec(candidate.trim())?.[1])
        .filter((address): address is string => !!address);
      if (found.length === 0) return state;
      return { kvmHostAddresses: [...new Set([...state.kvmHostAddresses, ...found])] };
    }),
  setHeld: held => set({ held }),
  setSweepTitle: sweepTitle =>
    set(state => ({
      sweepTitle,
      sweepDiagnosis: sweepTitle === null ? null : (state.sweepDiagnosis ?? state.diagnosis),
    })),
}));
