import type { Metadata } from "next";
import "./globals.css";
import { DM_Sans, Manrope } from "next/font/google";
import { cn } from "@/lib/utils";
import { TooltipProvider } from "@/components/ui/tooltip";
import { initializeTheme } from "@/lib/theme";

const dmSans = DM_Sans({ subsets: ["latin"], variable: "--font-dm-sans" });
const manrope = Manrope({ subsets: ["latin"], variable: "--font-manrope" });

export const metadata: Metadata = {
  applicationName: "Astra",
  title: "Astra · Central de vídeo",
  description: "Astra — monitoramento e acervo de vídeo da sua instalação",
};
export default function RootLayout({ children }: Readonly<{children: React.ReactNode}>) {
  return (
    <html lang="pt-BR" className={cn(dmSans.variable, manrope.variable)} suppressHydrationWarning>
      <head><script dangerouslySetInnerHTML={{ __html: initializeTheme }} /></head>
      {/* Browser extensions may add body attributes (e.g. cz-shortcut-listen) before hydration. */}
      <body className="astra-interface" suppressHydrationWarning>
        <TooltipProvider>{children}</TooltipProvider>
      </body>
    </html>
  );
}
