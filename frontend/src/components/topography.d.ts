import type { ComponentType } from "react";

type TopographyProps = {
  color?: string;
  speed?: number;
  brightness?: number;
  opacity?: number;
  dpr?: number;
};

declare const Topography: ComponentType<TopographyProps>;
export default Topography;
