"use client";

import { useEffect, useRef, useState, type FormEvent } from "react";
import { Search, Save } from "lucide-react";
import { useLiveView } from "@/components/live-view-context";
import { allPages } from "@/components/management";
import { FormDialog } from "@/components/form-dialog";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { api } from "@/lib/api";
import { onEnvironmentChange } from "@/lib/events";
import { mosaicPositions, mosaicSize, type LiveMosaic, type LivePosition } from "@/lib/live-mosaic";

const normalize = (text: string) => text.normalize("NFD").replace(/[\u0300-\u036f]/g, "").toLocaleLowerCase("pt-BR").trim();

export function LiveSavedViews() {
  const { tenantId, layoutSize, positions, selected, setSelected, loadMosaic } = useLiveView();
  const [views, setViews] = useState<LiveMosaic[]>([]);
  const [query, setQuery] = useState("");
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState("");
  const [retry, setRetry] = useState(0);
  const [draft, setDraft] = useState<{ positions: LivePosition[]; size: number } | null>(null);
  const [name, setName] = useState("");
  const [dialogError, setDialogError] = useState("");
  const [busy, setBusy] = useState(false);
  const [notice, setNotice] = useState("");
  const trigger = useRef<HTMLButtonElement>(null);
  const generation = useRef(0);
  const mounted = useRef(true);
  const saving = useRef(false);

  useEffect(() => {
    mounted.current = true;
    return () => { mounted.current = false; generation.current++; };
  }, []);

  useEffect(() => {
    if (!tenantId) return;
    let active = true;
    async function load() {
      const version = ++generation.current;
      setLoading(true);setError("");
      try {
        const items = await allPages<LiveMosaic>(`/tenants/${tenantId}/mosaics`);
        if (active && version === generation.current) setViews(items);
      } catch (reason) {
        if (active && version === generation.current) setError((reason as Error).message);
      } finally {
        if (active && version === generation.current) setLoading(false);
      }
    }
    void load();
    const unsubscribe = onEnvironmentChange([], () => void load());
    return () => { active = false; unsubscribe(); };
  }, [tenantId, retry]);

  const current = mosaicPositions(positions, layoutSize);
  const matchesSelected = selected && layoutSize === mosaicSize(selected.positions)
    && JSON.stringify(current) === JSON.stringify(mosaicPositions(selected.positions, layoutSize));
  const draftCameraCount = draft?.positions.filter(position => position.cameraId).length ?? 0;
  const filteredViews = views.filter(view => normalize(view.name).includes(normalize(query)))
    .sort((a, b) => a.name.localeCompare(b.name, "pt-BR"));

  function openSave() {
    setDraft({ positions: current, size: layoutSize });
    setName("");setDialogError("");setNotice("");
  }

  async function save(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (!draft || !tenantId || saving.current) return;
    const trimmed = name.trim();
    if (!trimmed) { setDialogError("Informe o nome da visualização.");return; }
    if (new TextEncoder().encode(trimmed).length > 200) { setDialogError("O nome é muito longo. Use um nome menor.");return; }
    saving.current = true;setBusy(true);setDialogError("");
    try {
      const created = await api<LiveMosaic>(`/tenants/${tenantId}/mosaics`, "POST", {
        name: trimmed, visibility: "personal", positions: draft.positions,
      }, true);
      if (!mounted.current) return;
      // Do not let an older list response replace the newly saved item.
      generation.current++;
      setViews(items => [...items.filter(item => item.id !== created.id), created]);
      setLoading(false);setError("");setSelected(created);setDraft(null);
      setNotice("Visualização salva.");
    } catch (reason) {
      if (mounted.current) setDialogError((reason as Error).message);
    } finally {
      saving.current = false;
      if (mounted.current) setBusy(false);
    }
  }

  return <div className="live-saved-views">
    <div className="live-camera-search">
      <Search aria-hidden="true"/>
      <Input type="search" aria-label="Buscar visualização" placeholder="Buscar visualização" value={query} onChange={event => setQuery(event.target.value)}/>
    </div>
    <Button ref={trigger} className="live-save-view" variant="outline" size="sm" onClick={openSave} disabled={!tenantId}>
      <Save aria-hidden="true"/><span>Salvar mosaico atual</span>
    </Button>
    {notice && <p className="live-camera-notice" role="status">{notice}</p>}
    <div className="live-saved-view-list">
      {error ? <div className="live-camera-notice" role="alert"><p>{error}</p><Button variant="ghost" size="sm" onClick={() => setRetry(value => value + 1)}>Tentar novamente</Button></div>
        : loading ? <p className="live-camera-notice" role="status">Carregando visualizações…</p>
        : !views.length ? <p className="live-camera-notice">Nenhuma visualização salva. Monte seu mosaico e salve para abri-lo novamente.</p>
        : !filteredViews.length ? <p className="live-camera-notice" role="status">Nenhuma visualização encontrada.</p>
        : <ul>{filteredViews.map(view => <li key={view.id}><button type="button" className="live-saved-view" title={view.name}
            aria-label={`Carregar visualização ${view.name}`} aria-pressed={!!matchesSelected && selected?.id === view.id}
            onClick={() => { loadMosaic(view);setNotice(""); }}>
            <span className="live-saved-view-name">{view.name}</span>
          </button></li>)}</ul>}
    </div>
    {draft && <FormDialog title="Salvar visualização" onClose={() => setDraft(null)} onSubmit={save} busy={busy} returnFocus={trigger.current}>
      {dialogError && <p className="live-view-form-error" role="alert">{dialogError}</p>}
      <Label className="field-label">Nome da visualização<Input autoFocus required maxLength={200} value={name} onChange={event => setName(event.target.value)} placeholder="Ex.: Portaria e estacionamento"/></Label>
      <p>Layout {draft.size}×{draft.size} · {draftCameraCount} {draftCameraCount === 1 ? "câmera" : "câmeras"}. Disponível para você neste ambiente.</p>
    </FormDialog>}
  </div>;
}
