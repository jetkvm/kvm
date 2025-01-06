import { Button } from "@components/Button";
import { cx } from "@/cva.config";
import { AvailableSidebarViews } from "@/hooks/stores";

export default function SidebarHeader({
  title,
  setSidebarView,
}: {
  title: string;
  setSidebarView: (view: AvailableSidebarViews | null) => void;
}) {
  return (
    <div className="flex items-center justify-between border-b border-b-slate-800/20 bg-white px-4 py-1.5 font-semibold text-black dark:bg-slate-900 dark:border-b-slate-300/20">
      <div className="min-w-0" style={{ flex: 1 }}>
        <h2 className="text-sm text-black truncate dark:text-white">{title}</h2>
      </div>
      <Button
        size="XS"
        theme="blank"
        text="Hide"
        LeadingIcon={({ className }) => (
          <svg
            className={cx(className, "rotate-180")}
            viewBox="0 0 22 22"
            fill="none"
            xmlns="http://www.w3.org/2000/svg"
          >
            <path
              fillRule="evenodd"
              clipRule="evenodd"
              d="M18 2H4C2.89543 2 2 2.89543 2 4V18C2 19.1046 2.89543 20 4 20H18C19.1046 20 20 19.1046 20 18V4C20 2.89543 19.1046 2 18 2ZM4 0C1.79086 0 0 1.79086 0 4V18C0 20.2091 1.79086 22 4 22H18C20.2091 22 22 20.2091 22 18V4C22 1.79086 20.2091 0 18 0H4Z"
              fill="currentColor"
            />
            <path
              fillRule="evenodd"
              clipRule="evenodd"
              d="M6 21L6 1L8 1L8 21H6Z"
              fill="currentColor"
            />
            <path
              fillRule="evenodd"
              clipRule="evenodd"
              d="M15.7803 7.21983C15.9208 7.36045 15.9997 7.55108 15.9997 7.74983C15.9997 7.94858 15.9208 8.1392 15.7803 8.27983L13.0603 10.9998L15.7803 13.7198C15.854 13.7885 15.9131 13.8713 15.9541 13.9633C15.9951 14.0553 16.0171 14.1546 16.0189 14.2553C16.0207 14.356 16.0022 14.456 15.9644 14.5494C15.9267 14.6428 15.8706 14.7276 15.7994 14.7989C15.7281 14.8701 15.6433 14.9262 15.5499 14.9639C15.4565 15.0017 15.3565 15.0202 15.2558 15.0184C15.1551 15.0166 15.0558 14.9946 14.9638 14.9536C14.8718 14.9126 14.789 14.8535 14.7203 14.7798L11.4703 11.5298C11.3299 11.3892 11.251 11.1986 11.251 10.9998C11.251 10.8011 11.3299 10.6105 11.4703 10.4698L14.7203 7.21983C14.8609 7.07938 15.0516 7.00049 15.2503 7.00049C15.4491 7.00049 15.6397 7.07938 15.7803 7.21983Z"
              fill="currentColor"
            />
          </svg>
        )}
        onClick={() => setSidebarView(null)}
      />
    </div>
  );
}
