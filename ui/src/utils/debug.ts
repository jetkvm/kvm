/**
 * Debug utilities for development mode logging
 */

// Check if we're in development mode
const isDevelopment = import.meta.env.DEV || import.meta.env.MODE === 'development';

/**
 * Development-only console.log wrapper
 * Only logs in development mode, silent in production
 */
export const devLog = (...args: unknown[]): void => {
  if (isDevelopment) {
    console.log(...args);
  }
};

/**
 * Development-only console.info wrapper
 * Only logs in development mode, silent in production
 */
export const devInfo = (...args: unknown[]): void => {
  if (isDevelopment) {
    console.info(...args);
  }
};

/**
 * Development-only console.warn wrapper
 * Only logs in development mode, silent in production
 */
export const devWarn = (...args: unknown[]): void => {
  if (isDevelopment) {
    console.warn(...args);
  }
};

/**
 * Development-only console.error wrapper
 * Always logs errors, but with dev prefix in development
 */
export const devError = (...args: unknown[]): void => {
  if (isDevelopment) {
    console.error('[DEV]', ...args);
  } else {
    console.error(...args);
  }
};

/**
 * Development-only debug function wrapper
 * Only executes the function in development mode
 */
export const devOnly = <T>(fn: () => T): T | undefined => {
  if (isDevelopment) {
    return fn();
  }
  return undefined;
};

/**
 * Check if we're in development mode
 */
export const isDevMode = (): boolean => isDevelopment;