"use client";

import { useEffect, useState } from "react";
import { Moon, Sun } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip";
import { themeStorageKey } from "@/lib/theme";

export function ThemeToggle({ variant = "ghost" }: { variant?: "ghost" | "outline" }) {
  const [dark, setDark] = useState(false);

  useEffect(() => {
    setDark(document.documentElement.classList.contains("dark"));
  }, []);

  function toggleTheme() {
    const nextDark = !document.documentElement.classList.contains("dark");
    document.documentElement.classList.toggle("dark", nextDark);
    setDark(nextDark);
    try {
      localStorage.setItem(themeStorageKey, nextDark ? "dark" : "light");
    } catch {
      // The chosen theme still applies for this visit when storage is unavailable.
    }
  }

  return <Tooltip>
    <TooltipTrigger asChild>
      <Button type="button" variant={variant} size="icon" className="theme-toggle" aria-label="Tema escuro" aria-pressed={dark} onClick={toggleTheme}>
        {dark ? <Sun aria-hidden="true" /> : <Moon aria-hidden="true" />}
      </Button>
    </TooltipTrigger>
    <TooltipContent>{dark ? "Usar tema claro" : "Usar tema escuro"}</TooltipContent>
  </Tooltip>;
}
