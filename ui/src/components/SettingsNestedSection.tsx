export default function SettingsNestedSection({
  children,
}: {
  children: React.ReactNode;
}) {
  return (
    <div className="ml-2 border-l border-slate-800/30 pl-4 dark:border-slate-300/30">
      {children}
    </div>
  );
}
