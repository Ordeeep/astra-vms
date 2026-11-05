import type { Metadata } from "next";
import "./globals.css";
import { Inter } from "next/font/google";
import { cn } from "@/lib/utils";
import { TooltipProvider } from "@/components/ui/tooltip";

const inter = Inter({subsets:['latin'],variable:'--font-inter'});

export const metadata: Metadata = {
  applicationName: "Astra",
  title: "Astra · Central de vídeo",
  description: "Astra — monitoramento e acervo de vídeo da sua instalação",
};
export default function RootLayout({ children }: Readonly<{children: React.ReactNode}>) {
  return (
    <html lang="pt-BR" className={cn("dark font-sans", inter.variable)}>
      {/* Browser extensions may add body attributes (e.g. cz-shortcut-listen) before hydration. */}
      <body className="astra-interface" suppressHydrationWarning>
        <TooltipProvider>{children}</TooltipProvider>
      </body>
    </html>
  );
}
