import { useNavigate, useParams, NavigateOptions } from "react-router-dom";
import { isOnDevice } from "../main";
import { useCallback, useMemo } from "react";

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

  // Function to generate the correct path
  const getPath = useCallback(
    (path: string): string => {
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
          console.error("No device ID found in params when generating path");
          throw new Error("No device ID found in params when generating path");
        }
        return `/devices/${deviceId}${normalizedPath}`;
      }
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
