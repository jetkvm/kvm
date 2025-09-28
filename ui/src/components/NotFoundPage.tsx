import { useTranslation } from "react-i18next";
import { ExclamationTriangleIcon } from "@heroicons/react/24/outline";

import EmptyCard from "@/components/EmptyCard";

export default function NotFoundPage() {
  const { t } = useTranslation();
  return (
    <div className="h-full w-full">
      <div className="flex h-full items-center justify-center">
        <div className="w-full max-w-2xl">
          <EmptyCard
            IconElm={ExclamationTriangleIcon}
            headline={t('Not_found')}
            description={t('The_page_you_were_looking_for_does_not_exist')}
          />
        </div>
      </div>
    </div>
  );
}
