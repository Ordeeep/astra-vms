"use client";

import {useEffect, useRef, useState, type ReactNode} from "react";
import {ChevronLeft, ChevronRight, LoaderCircle} from "lucide-react";
import {Button} from "@/components/ui/button";
import {Popover, PopoverContent, PopoverTrigger} from "@/components/ui/popover";
import {api} from "@/lib/api";
import {recordingCalendar, shiftRecordingDate, shiftRecordingMonth} from "@/lib/recording-navigation";

export function RecordingDatePicker({base, date, revision, onSelect, children}: {base: string; date: string; revision: number; onSelect: (date: string) => void; children: ReactNode}) {
  const [open, setOpen] = useState(false), [month, setMonth] = useState(date.slice(0, 7)), [retry, setRetry] = useState(0);
  const [snapshot, setSnapshot] = useState({key: "", days: [] as string[], loading: true, error: ""});
  const [focused, setFocused] = useState(date), grid = useRef<HTMLTableElement>(null);
  const key = `${base}/${month}/${revision}/${retry}`;
  useEffect(() => {
    if (!open) return;
    let active = true, busy = false;
    setSnapshot({key, days: [], loading: true, error: ""});
    const refresh = async () => {
      if (busy) return; busy = true;
      try {
        const visibleMonths = [...new Set(recordingCalendar(month).map(day => day.slice(0, 7)))];
        const values = await Promise.all(visibleMonths.map(value => api<{days: string[]}>(`${base}?month=${value}`, "GET", undefined, true)));
        if (active) setSnapshot({key, days: values.flatMap(value => value.days), loading: false, error: ""});
      } catch (error) { if (active) setSnapshot({key, days: [], loading: false, error: (error as Error).message}); }
      finally { busy = false; }
    };
    void refresh(); const timer = setInterval(() => void refresh(), 30_000);
    return () => { active = false; clearInterval(timer); };
  }, [base, month, key, open]);
  useEffect(() => { if (open) grid.current?.querySelector<HTMLButtonElement>(`[data-date="${focused}"]`)?.focus(); }, [focused, open]);
  const current = snapshot.key === key, loading = !current || snapshot.loading, error = current ? snapshot.error : "";
  const days = current && !error ? snapshot.days : [];
  const title = new Intl.DateTimeFormat("pt-BR", {month: "long", year: "numeric", timeZone: "UTC"}).format(new Date(`${month}-01T12:00:00Z`));
  return <Popover open={open} onOpenChange={value => { if (value) { setMonth(date.slice(0, 7)); setFocused(date); } setOpen(value); }}>
    <PopoverTrigger asChild><button type="button" className="recording-position-clock" aria-label="Escolher data das gravações" title="Escolher data das gravações">{children}</button></PopoverTrigger>
    <PopoverContent className="recording-calendar" align="start" side="top" aria-label="Filtro de data das gravações">
      <div className="recording-calendar-heading">
        <Button variant="ghost" size="icon" aria-label="Mês anterior" onClick={() => setMonth(value => shiftRecordingMonth(value, -1))}><ChevronLeft aria-hidden="true"/></Button>
        <strong aria-live="polite">{title}</strong>
        <Button variant="ghost" size="icon" aria-label="Próximo mês" onClick={() => setMonth(value => shiftRecordingMonth(value, 1))}><ChevronRight aria-hidden="true"/></Button>
      </div>
      <table ref={grid} role="grid" aria-label="Calendário de gravações" aria-busy={loading} onKeyDown={event => {
        if (!(event.target instanceof HTMLButtonElement)) return;
        const value = event.target.dataset.date; if (!value) return;
        const delta = {ArrowLeft: -1, ArrowRight: 1, ArrowUp: -7, ArrowDown: 7}[event.key];
        if (delta !== undefined) {
          event.preventDefault(); const next = shiftRecordingDate(value, delta); setFocused(next); setMonth(next.slice(0, 7));
        } else if (event.key === "PageUp" || event.key === "PageDown") {
          event.preventDefault(); const nextMonth = shiftRecordingMonth(month, event.key === "PageUp" ? -1 : 1);
          setMonth(nextMonth); setFocused(`${nextMonth}-01`);
        }
      }}>
        <thead><tr>{["Dom", "Seg", "Ter", "Qua", "Qui", "Sex", "Sáb"].map(day => <th key={day} scope="col">{day}</th>)}</tr></thead>
        <tbody>{Array.from({length: 6}, (_, row) => <tr key={row}>{recordingCalendar(month).slice(row * 7, row * 7 + 7).map(day => {
          const recorded = days.includes(day), label = day.split("-").reverse().join("/");
          return <td key={day} role="gridcell" aria-selected={date === day}><button type="button" data-date={day} data-recorded={recorded} data-outside={day.slice(0, 7) !== month}
            tabIndex={focused === day || !recordingCalendar(month).includes(focused) && day === `${month}-01` ? 0 : -1}
            aria-label={`${label}, ${loading ? "consultando gravações" : error ? "disponibilidade desconhecida" : recorded ? "com gravações" : "sem gravações"}`}
            onFocus={() => setFocused(day)} onClick={() => { onSelect(day); setOpen(false); }}>{Number(day.slice(-2))}</button></td>;
        })}</tr>)}</tbody>
      </table>
      {(loading || error) && <div className="recording-calendar-status" role={error ? "alert" : "status"}>{loading ? <><LoaderCircle className="animate-spin" aria-hidden="true"/>Consultando mês…</> : error ? <>{error}<Button variant="ghost" onClick={() => setRetry(value => value + 1)}>Tentar novamente</Button></> : null}</div>}
    </PopoverContent>
  </Popover>;
}
