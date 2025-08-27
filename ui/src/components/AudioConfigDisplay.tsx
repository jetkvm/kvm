import { cx } from "@/cva.config";

interface AudioConfig {
  Quality: number;
  Bitrate: number;
  SampleRate: number;
  Channels: number;
  FrameSize: string;
}

interface AudioConfigDisplayProps {
  config: AudioConfig;
  variant?: 'default' | 'success' | 'info';
  className?: string;
}

const variantStyles = {
  default: "bg-slate-50 text-slate-600 dark:bg-slate-700 dark:text-slate-400",
  success: "bg-green-50 text-green-600 dark:bg-green-900/20 dark:text-green-400",
  info: "bg-blue-50 text-blue-600 dark:bg-blue-900/20 dark:text-blue-400"
};

export function AudioConfigDisplay({ config, variant = 'default', className }: AudioConfigDisplayProps) {
  return (
    <div className={cx(
      "rounded-md p-2 text-xs",
      variantStyles[variant],
      className
    )}>
      <div className="grid grid-cols-2 gap-1">
        <span>Sample Rate: {config.SampleRate}Hz</span>
        <span>Channels: {config.Channels}</span>
        <span>Bitrate: {config.Bitrate}kbps</span>
        <span>Frame: {config.FrameSize}</span>
      </div>
    </div>
  );
}