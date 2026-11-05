import { AstraBrand } from "@/components/astra-brand";
import { Button } from "@/components/ui/button";

export default function NotFound() {
  return <main className="astra-state">
    <AstraBrand />
    <div className="astra-state-content">
      <span className="eyebrow">PÁGINA NÃO ENCONTRADA · 404</span>
      <h1>Este caminho não está disponível.</h1>
      <p>Confira o endereço ou volte à central para continuar.</p>
      <div className="astra-state-actions"><Button asChild><a href="/">Voltar à central</a></Button></div>
    </div>
  </main>;
}
