import { useRef, useState, useEffect } from "react";
import AnimateHeight, { Height } from "react-animate-height";

const AutoHeight = ({ children, ...props }: { children: React.ReactNode }) => {
  const [height, setHeight] = useState<Height>("auto");
  const contentDiv = useRef<HTMLDivElement | null>(null);

  useEffect(() => {
    const element = contentDiv.current as HTMLDivElement;

    const resizeObserver = new ResizeObserver(() => {
      setHeight(element.clientHeight);
    });

    resizeObserver.observe(element);

    return () => resizeObserver.disconnect();
  }, []);

  return (
    <AnimateHeight
      {...props}
      height={height}
      duration={300}
      contentClassName="h-fit p-px"
      contentRef={contentDiv}
      disableDisplayNone
    >
      {children}
    </AnimateHeight>
  );
};

export default AutoHeight;
