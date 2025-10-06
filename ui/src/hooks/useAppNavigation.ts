import { useNavigate, useParams } from "react-router";
import type { NavigateOptions } from "react-router";
import { useCallback, useMemo } from "react";

import { isOnDevice } from "../main";

/**
 * Generates the correct path based on whether the app is running on device or in cloud mode
 *
 */
export function getDeviceUiPath(path: string, deviceId?: string): string {
  // Check if it's a relative path (starts with . or ..)
  const isRelativePath = path.startsWith(".") || path === "";

  // If it's a relative path, don't modify it
  if (isRelativePath) return path;

  // Ensure absolute path starts with a slash
  const normalizedPath = path.startsWith("/") ? path : `/${path}`;

  if (isOnDevice) {
    return normalizedPath;
  } else {
    if (!deviceId) {
      console.error("No device ID provided when generating path in cloud mode");
      throw new Error("Device ID is required for cloud mode path generation");
    }
    return `/devices/${deviceId}${normalizedPath}`;
  }
}

/**
 * Hook that provides context-aware navigation and path generation
 * that works in both cloud and device modes.
 *
 * In cloud mode, paths are prefixed with /devices/:id
 * In device mode, paths start from the root
 * Relative paths (starting with . or ..) are preserved in both modes
 * Supports all React Router navigation options
 */
export function useDeviceUiNavigation() {
  const navigate = useNavigate();
  const params = useParams();

  // Get the device ID from params
  const deviceId = useMemo(() => params.id, [params.id]);

  // Use the standalone getPath function with the current deviceId
  const getPath = useCallback(
    (path: string): string => {
      return getDeviceUiPath(path, deviceId);
    },
    [deviceId],
  );

  // Function to navigate to the correct path with all options
  const navigateTo = useCallback(
    (path: string, options?: NavigateOptions) => {
      navigate(getPath(path), options);
    },
    [getPath, navigate],
  );

  return {
    navigateTo,
    getPath,
  };
}
