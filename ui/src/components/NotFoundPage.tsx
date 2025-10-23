import { ExclamationTriangleIcon } from "@heroicons/react/24/outline";

import EmptyCard from "@components/EmptyCard";
import { m } from "@localizations/messages.js";

export default function NotFoundPage() {
  return (
    <div className="h-full w-full">
      <div className="flex h-full items-center justify-center">
        <div className="w-full max-w-2xl">
          <EmptyCard
            IconElm={ExclamationTriangleIcon}
            headline={m.not_found()}
            description={m.page_not_found_description()}
          />
        </div>
      </div>
    </div>
  );
}
