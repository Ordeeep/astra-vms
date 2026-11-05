import './orbital-dawn.css';

// Original CSS scene, inspired by the public Horizon Bloom visual reference.
// Fixed positions keep the star field identical on the server and client.
const starFields = Array.from({ length: 3 }, (_, group) =>
  Array.from({ length: 32 }, (_, index) => {
    const seed = index * 73 + group * 191 + 17;
    return {
      left: `${(seed % 997) / 9.97}%`,
      top: `${((seed * 37) % 991) / 9.91}%`,
      opacity: 0.2 + (seed % 5) * 0.12,
      width: seed % 7 === 0 ? 2 : 1,
      height: seed % 7 === 0 ? 2 : 1,
    };
  }),
);

export default function OrbitalDawn() {
  return <div className="orbital-dawn" aria-hidden="true">
    <div className="orbital-dawn-stars">
      {starFields.map((stars, group) => <div className="orbital-dawn-starfield" key={group}>
        {stars.map((style, index) => <i key={index} style={style}/>)}
      </div>)}
    </div>
    <div className="orbital-dawn-orbit">
      <div className="orbital-dawn-light">
        <div className="orbital-dawn-atmosphere"/>
        <div className="orbital-dawn-airglow"/>
        <div className="orbital-dawn-planet"/>
        <div className="orbital-dawn-sun"/>
      </div>
    </div>
  </div>;
}
