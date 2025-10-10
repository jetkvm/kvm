import { useContext } from "react";

import { PermissionsContext } from "@/contexts/PermissionsContext";
import { Permission } from "@/types/permissions";

export interface PermissionsContextValue {
  permissions: Record<string, boolean>;
  isLoading: boolean;
  hasPermission: (permission: Permission) => boolean;
  hasAnyPermission: (...perms: Permission[]) => boolean;
  hasAllPermissions: (...perms: Permission[]) => boolean;
  isPrimary: () => boolean;
  isObserver: () => boolean;
  isPending: () => boolean;
}

export function usePermissions(): PermissionsContextValue {
  const context = useContext(PermissionsContext);

  if (context === undefined) {
    return {
      permissions: {},
      isLoading: true,
      hasPermission: () => false,
      hasAnyPermission: () => false,
      hasAllPermissions: () => false,
      isPrimary: () => false,
      isObserver: () => false,
      isPending: () => false,
    };
  }

  return context;
}
