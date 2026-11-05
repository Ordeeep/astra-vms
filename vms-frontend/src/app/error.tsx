"use client";

import { AstraBrand } from "@/components/astra-brand";
import { Button } from "@/components/ui/button";

export default function Error({ retry }: { error: unknown; retry: () => void }) {
  return <main className="astra-state">
    <AstraBrand />
    <div className="astra-state-content">
      <span className="eyebrow">CENTRAL DE VÍDEO</span>
      <h1>Não foi possível carregar esta tela.</h1>
      <p>Tente novamente para continuar de onde parou.</p>
      <div className="astra-state-actions">
        <Button onClick={retry}>Tentar novamente</Button>
        <Button asChild variant="outline"><a href="/">Voltar à central</a></Button>
      </div>
    </div>
  </main>;
}
