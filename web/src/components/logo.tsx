import { cn } from "@/lib/utils";

export function LogoMark({ className }: { className?: string }) {
  return (
    <svg viewBox="0 0 32 32" aria-hidden="true" className={cn("h-8 w-8", className)}>
      <rect width="32" height="32" rx="9" className="fill-primary" />
      <path
        d="M8.5 12.4 16 8.4l7.5 4v8.2L16 24.6l-7.5-4v-8.2Z"
        className="fill-none stroke-primary-foreground"
        strokeWidth="1.7"
        strokeLinejoin="round"
      />
      <path
        d="m8.8 12.7 7.2 4.1 7.2-4.1M16 16.8v7.4"
        className="fill-none stroke-primary-foreground/80"
        strokeWidth="1.7"
        strokeLinecap="round"
        strokeLinejoin="round"
      />
      <circle cx="23.4" cy="9.2" r="2.4" className="fill-primary-foreground" />
    </svg>
  );
}

export function Logo({ className, compact = false }: { className?: string; compact?: boolean }) {
  return (
    <span className={cn("inline-flex items-center gap-2.5", className)}>
      <LogoMark className="h-8 w-8 shrink-0" />
      {!compact && (
        <span className="text-[17px] font-semibold tracking-tight">
          mail<span className="text-muted-foreground">flow</span>
        </span>
      )}
    </span>
  );
}
