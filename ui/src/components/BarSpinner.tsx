import { cx } from "@/cva.config";

const BARS = 12;
const CYCLE_SECONDS = 1.2;

export default function BarSpinner({ className }: { className?: string }) {
  return (
    <span className={cx("relative inline-block shrink-0", className)} aria-hidden="true">
      {Array.from({ length: BARS }, (_, bar) => (
        <span
          key={bar}
          className="absolute top-1/2 left-1/2 h-[8%] w-[24%] animate-spinnerBar rounded-full bg-current motion-reduce:animate-none"
          style={{
            transform: `translate(-50%, -50%) rotate(${(360 / BARS) * bar}deg) translate(146%)`,
            animationDelay: `${(CYCLE_SECONDS / BARS) * bar - CYCLE_SECONDS}s`,
          }}
        />
      ))}
    </span>
  );
}
