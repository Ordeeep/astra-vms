"use client";

import { useEffect, useRef, useState, type FormEvent } from "react";
import { Pencil, Save, Search, Trash2 } from "lucide-react";
import { useLiveView } from "@/components/live-view-context";
import { allPages } from "@/components/management";
import { ConfirmationDialog, FormDialog } from "@/components/form-dialog";
import { Alert } from "@/components/ui/alert";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip";
import { ApiError, api } from "@/lib/api";
import { onEnvironmentChange } from "@/lib/events";
import { mosaicPositions, savedMosaicLayout, type LiveMosaic, type LivePosition, type MosaicLayout } from "@/lib/live-mosaic";

const normalize = (text: string) => text.normalize("NFD").replace(/[\u0300-\u036f]/g, "").toLocaleLowerCase("pt-BR").trim();

type ViewDialog =
  | { action: "create"; layout: MosaicLayout; positions: LivePosition[] }
  | { action: "rename" | "delete"; view: LiveMosaic };

export function LiveSavedViews() {
  const { tenantId, layout, positions, selected, setSelected, loadMosaic } = useLiveView();
  const [views, setViews] = useState<LiveMosaic[]>([]);
  const [query, setQuery] = useState("");
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState("");
  const [retry, setRetry] = useState(0);
  const [dialog, setDialog] = useState<ViewDialog | null>(null);
  const [name, setName] = useState("");
  const [dialogError, setDialogError] = useState("");
  const [busy, setBusy] = useState(false);
  const [notice, setNotice] = useState("");
  const trigger = useRef<HTMLButtonElement>(null);
  const search = useRef<HTMLInputElement>(null);
  const generation = useRef(0);
  const mounted = useRef(true);
  const submitting = useRef(false);

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

  const current = mosaicPositions(positions, layout.areas.length);
  const matchesSelected = selected && layout.id === savedMosaicLayout(selected).id
    && JSON.stringify(current) === JSON.stringify(mosaicPositions(selected.positions, layout.areas.length));
  const draft = dialog?.action === "create" ? dialog : null;
  const draftCameraCount = draft?.positions.filter(position => position.cameraId).length ?? 0;
  const filteredViews = views.filter(view => normalize(view.name).includes(normalize(query)))
    .sort((a, b) => a.name.localeCompare(b.name, "pt-BR"));

  function openDialog(next: ViewDialog, button: HTMLButtonElement) {
    trigger.current = button;
    setName(next.action === "create" ? "" : next.view.name);
    setDialogError("");
    setNotice("");
    setDialog(next);
  }

  async function submitViewChange(event?: FormEvent<HTMLFormElement>) {
    event?.preventDefault();
    if (!dialog || !tenantId || submitting.current) return;
    const trimmed = name.trim();
    if (dialog.action !== "delete") {
      if (!trimmed) { setDialogError("Informe o nome da visualização."); return; }
      if (new TextEncoder().encode(trimmed).length > 200) { setDialogError("O nome é muito longo. Use um nome menor."); return; }
    }
    submitting.current = true;
    setBusy(true);
    setDialogError("");
    try {
      const base = `/tenants/${tenantId}/mosaics`;
      if (dialog.action === "delete") {
        const { id, revision } = dialog.view;
        await api<void>(`${base}/${id}`, "DELETE", { revision }, true);
        if (!mounted.current) return;
        setViews(items => items.filter(item => item.id !== id));
        setSelected(item => item?.id === id ? null : item);
        setNotice("Visualização excluída.");
      } else {
        const view = dialog.action === "create"
          ? await api<LiveMosaic>(base, "POST", {
            name: trimmed, visibility: "personal", layout: dialog.layout.id, positions: dialog.positions,
          }, true)
          : await api<LiveMosaic>(`${base}/${dialog.view.id}`, "PUT", {
            name: trimmed, visibility: dialog.view.visibility, layout: dialog.view.layout,
            positions: dialog.view.positions, revision: dialog.view.revision,
          }, true);
        if (!mounted.current) return;
        setViews(items => [...items.filter(item => item.id !== view.id), view]);
        setSelected(item => dialog.action === "create" || item?.id === view.id ? view : item);
        setNotice(dialog.action === "create" ? "Visualização salva." : "Visualização renomeada.");
      }
      // Ignore list responses started before this mutation completed.
      generation.current++;
      setLoading(false);
      setError("");
      setDialog(null);
    } catch (reason) {
      if (!mounted.current) return;
      const code = reason instanceof ApiError ? reason.code : undefined;
      setDialogError(code === "revision_conflict"
        ? "A visualização foi alterada. Feche esta janela e abra a ação novamente."
        : (reason as Error).message);
      if (code === "revision_conflict" || code === "mosaic_not_found") {
        setRetry(value => value + 1);
      }
    } finally {
      submitting.current = false;
      if (mounted.current) setBusy(false);
    }
  }

  return <div className="live-saved-views">
    <div className="live-camera-tools">
      <div className="live-camera-search">
        <Search aria-hidden="true"/>
        <Input ref={search} type="search" aria-label="Buscar visualização" placeholder="Buscar visualização" value={query} onChange={event => setQuery(event.target.value)}/>
      </div>
      <Tooltip disableHoverableContent>
        <TooltipTrigger asChild>
          <Button type="button" className="live-save-view" variant="outline" size="icon"
            aria-label="Salvar mosaico atual" onClick={event => openDialog({ action: "create", positions: current, layout }, event.currentTarget)} disabled={!tenantId}>
            <Save aria-hidden="true"/>
          </Button>
        </TooltipTrigger>
        <TooltipContent side="top" sideOffset={4}>Salvar mosaico atual</TooltipContent>
      </Tooltip>
    </div>
    {notice && <p className="live-camera-notice" role="status">{notice}</p>}
    <div className="live-saved-view-list">
      {error ? <div className="live-camera-notice" role="alert"><p>{error}</p><Button variant="ghost" size="sm" onClick={() => setRetry(value => value + 1)}>Tentar novamente</Button></div>
        : loading ? <p className="live-camera-notice" role="status">Carregando visualizações…</p>
        : !views.length ? <p className="live-camera-notice">Nenhuma visualização salva. Monte seu mosaico e salve para abri-lo novamente.</p>
        : !filteredViews.length ? <p className="live-camera-notice" role="status">Nenhuma visualização encontrada.</p>
        : <ul>{filteredViews.map(view => <li key={view.id} className="live-saved-view-row"><Button type="button" variant="ghost" size="sm" className="live-saved-view" title={view.name}
            aria-label={`Carregar visualização ${view.name}`} aria-pressed={!!matchesSelected && selected?.id === view.id}
            onClick={() => { loadMosaic(view);setNotice(""); }}>
            <span>{view.name}</span>
          </Button>
          {view.editable && <div className="live-saved-view-icons">
            <Tooltip disableHoverableContent>
              <TooltipTrigger asChild>
                <Button type="button" variant="ghost" size="icon-sm" aria-label={`Editar nome da visualização ${view.name}`} aria-haspopup="dialog"
                  onClick={event => openDialog({ action: "rename", view }, event.currentTarget)}>
                  <Pencil aria-hidden="true"/>
                </Button>
              </TooltipTrigger>
              <TooltipContent side="top" sideOffset={4}>Editar nome</TooltipContent>
            </Tooltip>
            <Tooltip disableHoverableContent>
              <TooltipTrigger asChild>
                <Button type="button" variant="ghost" size="icon-sm" aria-label={`Excluir visualização ${view.name}`} aria-haspopup="dialog"
                  onClick={event => openDialog({ action: "delete", view }, event.currentTarget)}>
                  <Trash2 aria-hidden="true"/>
                </Button>
              </TooltipTrigger>
              <TooltipContent side="top" sideOffset={4}>Excluir visualização</TooltipContent>
            </Tooltip>
          </div>}
        </li>)}</ul>}
    </div>
    {dialog?.action === "delete"
      ? <ConfirmationDialog title="Excluir visualização?"
        description={<>A visualização <strong className="live-view-delete-name">“{dialog.view.name}”</strong> será excluída. Deseja continuar?</>}
        confirmLabel={busy ? "Excluindo…" : "Excluir visualização"} busy={busy}
        onCancel={() => setDialog(null)} onConfirm={() => void submitViewChange()}
        returnFocus={trigger.current} fallbackFocus={() => search.current}>
        {dialogError && <Alert className="live-view-dialog-error" variant="destructive">{dialogError}</Alert>}
      </ConfirmationDialog>
      : dialog && <FormDialog title={draft ? "Salvar visualização" : "Editar nome da visualização"}
        onClose={() => setDialog(null)} onSubmit={submitViewChange} busy={busy}
        saveDisabled={dialog.action === "rename" && name.trim() === dialog.view.name}
        hasChanges={dialog.action === "rename" ? name.trim() !== dialog.view.name : undefined}
        returnFocus={trigger.current} fallbackFocus={() => search.current}>
        <Label className="field-label">Nome da visualização
          <Input autoFocus required maxLength={200} value={name}
            aria-describedby={dialogError ? "live-view-name-error" : undefined}
            onChange={event => { setName(event.target.value); setDialogError(""); }}
            placeholder="Ex.: Portaria e estacionamento"/>
        </Label>
        {dialogError && <Alert id="live-view-name-error" className="live-view-dialog-error" variant="destructive">{dialogError}</Alert>}
        {draft && <p>Layout {draft.layout.label} · {draftCameraCount} {draftCameraCount === 1 ? "câmera" : "câmeras"}. Disponível para você neste ambiente.</p>}
      </FormDialog>}
  </div>;
}
