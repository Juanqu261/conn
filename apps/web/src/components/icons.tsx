import type { ReactNode, SVGProps } from "react";

// Self-hosted, decorative icons. Every icon is aria-hidden: the text beside
// it carries the meaning (requirement 4.2).

type IconProps = Omit<SVGProps<SVGSVGElement>, "children"> & { size?: number };

function Icon({ size = 18, children, ...props }: IconProps & { children: ReactNode }) {
  return (
    <svg
      width={size}
      height={size}
      viewBox="0 0 24 24"
      fill="none"
      stroke="currentColor"
      strokeWidth={1.8}
      strokeLinecap="round"
      strokeLinejoin="round"
      aria-hidden="true"
      focusable="false"
      {...props}
    >
      {children}
    </svg>
  );
}

export const BoltIcon = (props: IconProps) => (
  <Icon {...props}>
    <path d="M13.5 2 4 14h7l-1.5 8L20 10h-7z" fill="currentColor" stroke="none" />
  </Icon>
);

export const FlaskIcon = (props: IconProps) => (
  <Icon {...props}>
    <path d="M9 3h6M10 3v6L4.5 18.5A2 2 0 0 0 6.2 21.5h11.6a2 2 0 0 0 1.7-3L14 9V3" />
    <path d="M7.5 15h9" />
  </Icon>
);

export const PersonIcon = (props: IconProps) => (
  <Icon {...props}>
    <circle cx="12" cy="8" r="4" />
    <path d="M4 21c1.5-4 4.5-6 8-6s6.5 2 8 6" />
  </Icon>
);

export const SearchIcon = (props: IconProps) => (
  <Icon {...props}>
    <circle cx="11" cy="11" r="6.5" />
    <path d="m20 20-4.2-4.2" />
  </Icon>
);

export const ChevronIcon = (props: IconProps) => (
  <Icon {...props}>
    <path d="m7 10 5 5 5-5" />
  </Icon>
);

export const MoreIcon = (props: IconProps) => (
  <Icon {...props}>
    <circle cx="5" cy="12" r="1.3" fill="currentColor" />
    <circle cx="12" cy="12" r="1.3" fill="currentColor" />
    <circle cx="19" cy="12" r="1.3" fill="currentColor" />
  </Icon>
);

export const StitchIcon = (props: IconProps) => (
  <Icon {...props}>
    <path d="M3 12h18" strokeDasharray="3 3" />
    <path d="M6 8v8M12 8v8M18 8v8" />
  </Icon>
);

export const JarIcon = (props: IconProps) => (
  <Icon {...props}>
    <path d="M8 3h8M9 3v3a5 5 0 0 0-3 4.6V19a2 2 0 0 0 2 2h8a2 2 0 0 0 2-2v-8.4A5 5 0 0 0 15 6V3" />
    <path d="M6 13h12" />
  </Icon>
);

export const QuestionIcon = (props: IconProps) => (
  <Icon {...props}>
    <circle cx="12" cy="12" r="9" />
    <path d="M9.5 9.5a2.5 2.5 0 1 1 3.5 2.3c-.6.3-1 .9-1 1.6v.6M12 17.5v.01" />
  </Icon>
);

export const FogIcon = (props: IconProps) => (
  <Icon {...props}>
    <path d="M3 9h12M6 13h15M3 17h11" />
  </Icon>
);

export const CheckIcon = (props: IconProps) => (
  <Icon {...props}>
    <path d="m5 12.5 4.5 4.5L19 7" />
  </Icon>
);

export const CrossIcon = (props: IconProps) => (
  <Icon {...props}>
    <path d="M6 6l12 12M18 6 6 18" />
  </Icon>
);

export const DiamondIcon = (props: IconProps) => (
  <Icon {...props}>
    <path d="M12 3 21 12 12 21 3 12z" />
  </Icon>
);

export const CircleIcon = (props: IconProps) => (
  <Icon {...props}>
    <circle cx="12" cy="12" r="7" />
  </Icon>
);

export const DotIcon = (props: IconProps) => (
  <Icon {...props}>
    <circle cx="12" cy="12" r="5" fill="currentColor" stroke="none" />
  </Icon>
);

export const MoonIcon = (props: IconProps) => (
  <Icon {...props}>
    <path d="M20 14.5A8 8 0 0 1 9.5 4a8 8 0 1 0 10.5 10.5z" />
  </Icon>
);

export const SunIcon = (props: IconProps) => (
  <Icon {...props}>
    <circle cx="12" cy="12" r="4" />
    <path d="M12 2v2M12 20v2M4.9 4.9l1.4 1.4M17.7 17.7l1.4 1.4M2 12h2M20 12h2M4.9 19.1l1.4-1.4M17.7 6.3l1.4-1.4" />
  </Icon>
);

export const DoorIcon = (props: IconProps) => (
  <Icon {...props}>
    <path d="M14 3H6v18h8M10 12h11M17 8l4 4-4 4" />
  </Icon>
);

export const KeyboardIcon = (props: IconProps) => (
  <Icon {...props}>
    <rect x="2.5" y="6" width="19" height="12" rx="2" />
    <path d="M6 10h.01M10 10h.01M14 10h.01M18 10h.01M7 14h10" />
  </Icon>
);

export const TombstoneIcon = (props: IconProps) => (
  <Icon {...props}>
    <path d="M6 21V10a6 6 0 0 1 12 0v11M4 21h16M12 9v6M9.5 11.5h5" />
  </Icon>
);
