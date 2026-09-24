// The line icons of the mockups (docs/mockups/generate.py icon()), drawn on a
// 16-unit grid with the current text colour, so a control's colour is set in
// CSS once for its label and its icon.
const paths = {
  search: <><circle cx="7" cy="7" r="4.5" /><path d="M10.5 10.5 14 14" /></>,
  inbox: <><path d="M2 9h3.5l1 2h3l1-2H14" /><path d="M3.5 3h9l1.5 6v4H2V9z" /></>,
  home: <><path d="M2.5 7.5 8 3l5.5 4.5V13a.5.5 0 0 1-.5.5H3a.5.5 0 0 1-.5-.5z" /><path d="M6.5 13.5v-4h3v4" /></>,
  list: (
    <>
      <path d="M5 4h9M5 8h9M5 12h9" />
      <circle cx="2.5" cy="4" r=".75" />
      <circle cx="2.5" cy="8" r=".75" />
      <circle cx="2.5" cy="12" r=".75" />
    </>
  ),
  clock: <><circle cx="8" cy="8" r="5.5" /><path d="M8 5v3.5l2.5 1.5" /></>,
  bell: <><path d="M4 11V7.5a4 4 0 0 1 8 0V11l1 1.5H3z" /><path d="M6.5 13.5a1.5 1.5 0 0 0 3 0" /></>,
  user: <><circle cx="8" cy="5.5" r="2.75" /><path d="M2.75 14a5.25 5.25 0 0 1 10.5 0" /></>,
  more: <><circle cx="3.5" cy="8" r="1" /><circle cx="8" cy="8" r="1" /><circle cx="12.5" cy="8" r="1" /></>,
  back: <path d="m10 3-5 5 5 5" />,
  link: (
    <path d="M6.5 9.5 9.5 6.5M7 4.5l1-1a2.5 2.5 0 0 1 3.5 3.5l-1 1M9 11.5l-1 1a2.5 2.5 0 0 1-3.5-3.5l1-1" />
  ),
};

export type IconName = keyof typeof paths;

export function Icon({ name, size = 16 }: { name: IconName; size?: number }) {
  return (
    <svg
      class="icon"
      width={size}
      height={size}
      viewBox="0 0 16 16"
      fill="none"
      stroke="currentColor"
      stroke-width="1.5"
      stroke-linecap="round"
      stroke-linejoin="round"
      aria-hidden="true"
    >
      {paths[name]}
    </svg>
  );
}
