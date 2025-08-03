import React from 'react';
import clsx from 'clsx';

interface AudioLevelMeterProps {
  level: number; // 0-100 percentage
  isActive: boolean;
  className?: string;
  size?: 'sm' | 'md' | 'lg';
  showLabel?: boolean;
}

export const AudioLevelMeter: React.FC<AudioLevelMeterProps> = ({
  level,
  isActive,
  className,
  size = 'md',
  showLabel = true
}) => {
  const sizeClasses = {
    sm: 'h-1',
    md: 'h-2',
    lg: 'h-3'
  };

  const getLevelColor = (level: number) => {
    if (level < 20) return 'bg-green-500';
    if (level < 60) return 'bg-yellow-500';
    return 'bg-red-500';
  };

  const getTextColor = (level: number) => {
    if (level < 20) return 'text-green-600 dark:text-green-400';
    if (level < 60) return 'text-yellow-600 dark:text-yellow-400';
    return 'text-red-600 dark:text-red-400';
  };

  return (
    <div className={clsx('space-y-1', className)}>
      {showLabel && (
        <div className="flex justify-between text-xs">
          <span className="text-slate-500 dark:text-slate-400">
            Microphone Level
          </span>
          <span className={clsx(
            'font-mono',
            isActive ? getTextColor(level) : 'text-slate-400 dark:text-slate-500'
          )}>
            {isActive ? `${Math.round(level)}%` : 'No Signal'}
          </span>
        </div>
      )}
      
      <div className={clsx(
        'w-full rounded-full bg-slate-200 dark:bg-slate-700',
        sizeClasses[size]
      )}>
        <div
          className={clsx(
            'rounded-full transition-all duration-150 ease-out',
            sizeClasses[size],
            isActive ? getLevelColor(level) : 'bg-slate-300 dark:bg-slate-600'
          )}
          style={{
            width: isActive ? `${Math.min(100, Math.max(2, level))}%` : '0%'
          }}
        />
      </div>
      
      {/* Peak indicators */}
      <div className="flex justify-between text-xs text-slate-400 dark:text-slate-500">
        <span>0%</span>
        <span>50%</span>
        <span>100%</span>
      </div>
    </div>
  );
};