-- A manual "skip" command (Home Assistant "Skip Queue" number) pins the exact
-- image the frame should serve on its next pull, letting the user jump N steps
-- forward/backward in the rotation. The next ordered pull serves this image and
-- clears the pin, then rotation continues normally from there. 0 = no pin.
ALTER TABLE devices ADD COLUMN pending_next_image_id INTEGER DEFAULT 0;
