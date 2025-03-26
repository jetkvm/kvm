import { ExclamationTriangleIcon } from "@heroicons/react/24/outline";

import EmptyCard from "@/components/EmptyCard";

export default function NotFoundPage() {
  return (
    <div className="h-full w-full">
      <div className="flex h-full items-center justify-center">
        <div className="w-full max-w-2xl">
          <EmptyCard
            IconElm={ExclamationTriangleIcon}
            headline="Not found"
            description="The page you were looking for does not exist."
          />
        </div>
      </div>
    </div>
  );
}
