import type { ComponentProps } from "react";
import { cn } from "@/lib/utils";

// Approved Astra direction 02. Inline vectors keep branding available in the
// initial loading screen and preserve the same geometry at every display size.
export function AstraMark({ className, ...props }: ComponentProps<"svg">) {
  return <svg viewBox="48 86 416 340" fill="currentColor" aria-hidden="true"
    className={cn("astra-mark", className)} {...props}>
    <path d="M256 86 308 171 148 426H48Z" />
    <path d="M319 191 464 426H364L270 274Z" />
    <path d="M256 286C256 300 246 310 232 310C246 310 256 320 256 334C256 320 266 310 280 310C266 310 256 300 256 286Z" />
  </svg>;
}

export function AstraWordmark({ className, ...props }: ComponentProps<"svg">) {
  return <svg viewBox="0 0 884 120" fill="currentColor" aria-hidden="true"
    className={cn("astra-wordmark", className)} {...props}>
    <path d="M0 120 58 0H82L140 120H109L70 43 31 120Z" />
    <path transform="translate(190 0)" d="M128 14H39C5 14 5 60 39 60H89C123 60 123 106 89 106H0"
      fill="none" stroke="currentColor" strokeWidth="28" strokeLinecap="butt" />
    <path transform="translate(368 0)" d="M0 0H130V27H79V120H51V27H0Z" />
    <path transform="translate(548 0)" fillRule="evenodd"
      d="M0 0H78C111 0 132 18 132 43C132 63 120 76 100 80L146 120H104L60 82H28V120H0ZM28 26V56H76C94 56 104 51 104 42C104 32 94 26 76 26Z" />
    <path transform="translate(744 0)" d="M0 120 58 0H82L140 120H109L70 43 31 120Z" />
  </svg>;
}

export function AstraBrand() {
  return <span className="brand" role="img" aria-label="Astra · Central de vídeo">
    <AstraMark />
    <span><AstraWordmark /><span className="brand-caption">CENTRAL DE VÍDEO</span></span>
  </span>;
}
