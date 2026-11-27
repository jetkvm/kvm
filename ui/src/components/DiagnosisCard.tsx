import { ExclamationTriangleIcon } from "@heroicons/react/24/solid";
import { ArrowPathIcon, CheckIcon, XCircleIcon } from "@heroicons/react/16/solid";

import { GridCard } from "@components/Card";
import { Button, LinkButton } from "@components/Button";
import { GitHubIcon } from "@components/Icons";
import BarSpinner from "@components/BarSpinner";
import { cx } from "@/cva.config";
import { questionNames, type CardAction, type Diagnosis, type Step } from "@/diagnosis/diagnose";
import { m } from "@localizations/messages.js";

const ISSUE_URL = "https://github.com/jetkvm/kvm/issues/new/choose";
const FIRST_TIMED_QUESTION = 3;

const stepsHeading = {
  certain: m.diagnosis_steps_certain,
  try_first: m.diagnosis_steps_try_first,
  issue: () => null,
};

const actionLabels: Partial<Record<CardAction, () => string>> = {
  wake: m.diagnosis_action_wake,
  play: m.diagnosis_action_play,
  restoreEdid: m.diagnosis_action_restore,
  update: m.diagnosis_action_check_updates,
  restart: m.diagnosis_action_restart,
  videoSettings: m.diagnosis_action_video_settings,
  edidSweep: m.diagnosis_action_start,
};

const issueHeading = {
  certain: m.diagnosis_if_nothing_works,
  try_first: m.diagnosis_if_nothing_works,
  issue: m.diagnosis_cause_unknown,
};

interface Busy {
  action: CardAction;
  text: string;
}

interface DiagnosisCardProps {
  diagnosis: Diagnosis;
  onAction?: (action: CardAction) => void;
  busy?: Busy;
}

function Inline({ text }: { text: string }) {
  return text.split("`").map((part, index) =>
    index % 2 === 1 ? (
      <code
        key={index}
        className="rounded-sm bg-slate-200/70 px-1 py-0.5 font-mono text-[12px] whitespace-nowrap text-slate-900 select-all dark:bg-slate-700 dark:text-slate-100"
      >
        {part}
      </code>
    ) : (
      part
    ),
  );
}

function Steps({
  steps,
  onAction,
  busy,
}: {
  steps: Step[];
  onAction?: (action: CardAction) => void;
  busy?: Busy;
}) {
  let number = 0;
  return (
    <ol className="space-y-3">
      {steps.map(step => {
        if (!step.alternative) number += 1;
        return (
          <li key={step.title} className="flex gap-x-3">
            <span
              className={cx(
                "mt-px flex h-5 min-w-5 shrink-0 items-center justify-center rounded-full text-[11px] font-semibold",
                step.alternative
                  ? "text-slate-500 dark:text-slate-400"
                  : "bg-blue-700 text-white dark:bg-blue-600",
              )}
            >
              {step.alternative ? m.diagnosis_step_or() : number}
            </span>
            <div className="space-y-1">
              <div className="font-semibold text-black dark:text-white">{step.title}</div>
              {(step.detail || step.link) && (
                <p className="leading-relaxed text-slate-600 dark:text-slate-400">
                  {step.detail && <Inline text={step.detail} />}
                  {step.detail && step.link && " "}
                  {step.link && (
                    <a
                      href={step.link.href}
                      target="_blank"
                      rel="noreferrer"
                      className="font-medium whitespace-nowrap text-blue-700 hover:underline dark:text-blue-400"
                    >
                      {step.link.label}
                    </a>
                  )}
                </p>
              )}
              {step.action && onAction && (
                <div className={cx("flex items-center gap-x-2", busy && "pointer-events-none")}>
                  <Button
                    size="XS"
                    theme="light"
                    text={actionLabels[step.action]?.()}
                    disabled={!!busy}
                    onClick={() => onAction(step.action!)}
                  />
                  {busy?.action === step.action && (
                    <span className="flex items-center gap-x-1.5 text-xs text-slate-600 dark:text-slate-400">
                      <BarSpinner className="h-3.5 w-3.5" />
                      {busy.text}
                    </span>
                  )}
                </div>
              )}
            </div>
          </li>
        );
      })}
    </ol>
  );
}

export function Checks({ diagnosis }: { diagnosis: Diagnosis }) {
  const names = questionNames();
  const stoppedAt = diagnosis.status === "ok" ? names.length + 1 : diagnosis.question;
  return (
    <ul className="space-y-1 text-[13px]">
      {names.map((name, index) => {
        const question = index + 1;
        const passed = question < stoppedAt;
        const current = question === stoppedAt;
        return (
          <li
            key={name}
            className={cx(
              "flex items-center gap-x-2 whitespace-nowrap",
              passed && "text-slate-600 dark:text-slate-400",
              current && "font-semibold text-black dark:text-white",
              !passed && !current && "text-slate-400 dark:text-slate-500",
            )}
          >
            <span className="flex h-4 w-4 shrink-0 items-center justify-center">
              {passed && <CheckIcon className="h-3.5 w-3.5 text-green-600 dark:text-green-500" />}
              {current && diagnosis.status === "failed" && (
                <XCircleIcon className="h-4 w-4 text-red-600 dark:text-red-500" />
              )}
              {current && diagnosis.status === "waiting" && (
                <BarSpinner className="h-3.5 w-3.5 text-slate-600 dark:text-slate-300" />
              )}
              {!passed && !current && (
                <span className="h-1 w-1 rounded-full bg-slate-300 dark:bg-slate-600" />
              )}
            </span>
            {name}
          </li>
        );
      })}
    </ul>
  );
}

function Body({
  diagnosis,
  onAction,
  busy,
}: Omit<DiagnosisCardProps, "diagnosis"> & {
  diagnosis: Extract<Diagnosis, { status: "failed" }>;
}) {
  const reconnect = (size: "XS" | "SM") =>
    diagnosis.canReconnect &&
    onAction && (
      <Button
        size={size}
        theme="light"
        text={m.diagnosis_reconnect()}
        LeadingIcon={ArrowPathIcon}
        onClick={() => onAction("reconnect")}
      />
    );
  const hasSteps = diagnosis.steps.length > 0;

  return (
    <div className="space-y-5">
      <div className="space-y-1.5">
        <div className="flex items-center gap-x-2">
          <ExclamationTriangleIcon className="h-6 w-6 shrink-0 text-yellow-500" />
          <h2 className="text-xl font-bold text-black dark:text-white">{diagnosis.title}</h2>
        </div>
        <p className="text-slate-700 dark:text-slate-300">{diagnosis.summary}</p>
      </div>

      {hasSteps && (
        <div className="space-y-3">
          <div className="flex items-center justify-between gap-x-3">
            <h3 className="text-xs font-semibold text-slate-500 dark:text-slate-400">
              {stepsHeading[diagnosis.level]()}
            </h3>
            {reconnect("XS")}
          </div>
          <Steps steps={diagnosis.steps} onAction={onAction} busy={busy} />
        </div>
      )}

      <div className="flex flex-wrap items-center justify-between gap-x-4 gap-y-3 rounded-md border border-slate-800/20 bg-white/70 px-4 py-3 dark:border-slate-300/20 dark:bg-slate-800/70">
        <div className="space-y-0.5">
          <div className="font-semibold text-black dark:text-white">
            {issueHeading[diagnosis.level]()}
          </div>
          <p className="text-slate-600 dark:text-slate-400">{m.diagnosis_report_on_github()}</p>
        </div>
        <div className={cx("flex items-center gap-x-2", busy && "pointer-events-none opacity-50")}>
          {!hasSteps && reconnect("SM")}
          <LinkButton
            to={ISSUE_URL}
            theme="light"
            text={m.diagnosis_create_issue()}
            LeadingIcon={GitHubIcon}
            size="SM"
          />
        </div>
      </div>
    </div>
  );
}

export default function DiagnosisCard({ diagnosis, onAction, busy }: DiagnosisCardProps) {
  if (diagnosis.status === "ok") return null;

  if (diagnosis.status === "waiting") {
    return (
      <GridCard cardClassName="h-full pointer-events-auto outline-hidden!">
        <div
          data-testid="connection-card"
          data-status={diagnosis.status}
          data-question={diagnosis.question}
          className="flex h-full w-full flex-col items-center justify-center gap-y-1 rounded-md border border-slate-800/30 p-6 text-center dark:border-slate-300/20"
        >
          <div className="flex h-12 w-12 items-center justify-center">
            <BarSpinner className="h-6 w-6 text-slate-600 dark:text-slate-300" />
          </div>
          <p className="text-sm text-slate-700 dark:text-slate-300">{diagnosis.title}</p>
          <div className="mt-3 flex items-center gap-x-1" aria-hidden="true">
            {questionNames()
              .slice(FIRST_TIMED_QUESTION - 1)
              .map((name, index) => {
                const question = index + FIRST_TIMED_QUESTION;
                return (
                  <span
                    key={name}
                    className={cx(
                      "h-0.5 w-5 rounded-full transition-colors duration-500",
                      question < diagnosis.question && "bg-blue-700 dark:bg-blue-400",
                      question === diagnosis.question &&
                        "animate-pulse bg-blue-700/60 dark:bg-blue-400/70",
                      question > diagnosis.question && "bg-slate-300 dark:bg-slate-700",
                    )}
                  />
                );
              })}
          </div>
        </div>
      </GridCard>
    );
  }

  return (
    <GridCard cardClassName="h-full pointer-events-auto outline-hidden!">
      <div
        data-testid="connection-card"
        data-status={diagnosis.status}
        data-question={diagnosis.question}
        className="h-full w-full overflow-y-auto rounded-md border border-slate-800/30 text-left text-sm dark:border-slate-300/20"
      >
        <div className="flex min-h-full items-center justify-center p-6 md:p-8">
          <div className="grid w-full max-w-3xl gap-8 md:grid-cols-[minmax(0,1fr)_13rem]">
            <div className="md:self-center">
              <Body diagnosis={diagnosis} onAction={onAction} busy={busy} />
            </div>
            <div className="flex flex-col justify-center border-t border-slate-800/20 pt-6 md:self-stretch md:border-t-0 md:border-l md:pt-0 md:pl-8 dark:border-slate-300/20">
              <Checks diagnosis={diagnosis} />
            </div>
          </div>
        </div>
      </div>
    </GridCard>
  );
}
