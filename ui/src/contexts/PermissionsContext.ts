import { createContext } from "react";

import { PermissionsContextValue } from "@/hooks/usePermissions";

export const PermissionsContext = createContext<PermissionsContextValue | undefined>(undefined);
